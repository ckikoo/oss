package timer

import (
	"context"
	"errors"
	"fmt"
	"oss/adaptor"
	"oss/adaptor/redis"
	gormAdmin "oss/adaptor/repo/admin/gorm"
	gormAsync "oss/adaptor/repo/async/gorm"
	gormBucket "oss/adaptor/repo/bucket/gorm"
	gormMultipart "oss/adaptor/repo/multipart/gorm"
	gormObject "oss/adaptor/repo/object/gorm"
	"oss/adaptor/repo/repoerr"
	"oss/adaptor/storage"
	"oss/adaptor/tx"
	"oss/consts"
	"oss/service/do"
	videoSvc "oss/service/video"
	"oss/utils/pool"
	"strconv"
	"strings"
	"time"

	"github.com/gogf/gf/util/gconv"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"go.uber.org/zap"
)

func handlerTask(ctx context.Context, adaptor adaptor.IAdaptor) {
	// 从 Redis 队列中取出任务 ID
	redisTask := redis.NewTask(adaptor)
	taskRepo := gormAsync.NewAsyncTaskRepo(adaptor.GetGORM())
	storageImp := adaptor.GetStorage()
	multipart := gormMultipart.NewObjectRepo(adaptor.GetGORM())
	fileRepo := gormObject.NewObjectRepo(adaptor)
	taskLocker := redis.NewLock(adaptor)
	uinfoRepo := gormAdmin.NewUserRepo(adaptor)
	bucketRepo := gormBucket.NewBucketRepo(adaptor)
	objRepo := gormObject.NewObjectRepo(adaptor)
	txManager := adaptor.GetTxManager()
	videoProcessor := videoSvc.NewProcessor(adaptor)
	taskIDs, err := redisTask.DequeueTask(ctx, 50, time.Second*5)
	videoScheduler := videoSvc.NewScheduler(adaptor)
	if err != nil {
		log.Error("timer fail to dequeue task", zap.Error(err))
		return
	}
	if len(taskIDs) == 0 {
		return
	}

	locker := redis.NewLock(adaptor)

	p := pool.NewPoolWithSize(5)
	for _, taskID := range taskIDs {
		taskID := taskID // 每次迭代创建新变量

		if err := p.RunGo(func() {
			workerID := uuid.NewString()
			taskLockKey := buildLockKey(consts.ServerName, "task", strconv.FormatInt(taskID, 10))
			ok, err := taskLocker.AcquireLock(ctx, taskLockKey, workerID, time.Second*30)
			if err != nil {
				log.Error("timer.handlerTask fail to acquire lock", zap.Error(err), zap.Int64("taskID", taskID))
				return
			}
			if !ok {
				// 锁被占用，跳过
				return
			}

			defer func() {
				if err := taskLocker.ReleaseLock(ctx, taskLockKey, workerID); err != nil {
					log.Error("timer.handlerTask fail to release lock", zap.Error(err), zap.Int64("taskID", taskID))
				}
			}()

			taskCtx, cancelTask := context.WithCancel(ctx)
			defer cancelTask()

			watchCtx, stopWatchdog := context.WithCancel(ctx)
			defer stopWatchdog() // goroutine 随任务结束而退出

			go func() {
				ticker := time.NewTicker(time.Second * 10) // 每 10 秒续期一次
				defer ticker.Stop()
				for {
					select {
					case <-watchCtx.Done():
						return
					case <-ticker.C:
						if err := taskLocker.RefreshLock(ctx, taskLockKey, workerID, time.Second*30); err != nil {
							// 续期失败说明锁已丢失，应中断任务
							cancelTask()
							return
						}
					}
				}
			}()

			claimed, task, err := taskRepo.ClaimAsyncTask(taskCtx, taskID)
			if err != nil {
				log.Error("timer.handlerTask fail to claim async task", zap.Error(err), zap.Int64("taskID", taskID))
				return
			}
			if !claimed || task == nil {
				return
			}

			uploadID := task.BizID

			switch task.TaskType {
			case consts.TaskTypePhysicalMerge:
				if uploadID == "" {
					_ = updateTaskStatus(taskCtx, taskRepo, task.ID, consts.TaskStatusFailed, "task biz_id is empty")
					return
				}
				info, err := multipart.GetMultipartUploadByID(taskCtx, task.UserId, uploadID)
				if err != nil {
					log.Error("timer.handlerTask fail to get multipart upload info", zap.Error(err), zap.Int64("taskID", taskID))
					return
				}

				resourcekey := buildLockKey(consts.ServerName, "multipart", info.BucketName, info.ObjectKey)

				get, err := locker.AcquireLock(taskCtx, resourcekey, workerID, time.Minute*10)
				if err != nil {
					log.Error("timer.handlerTask fail to acquire multipart lock", zap.Error(err), zap.String("resourceKey", resourcekey), zap.Int64("taskID", taskID))
					return
				}

				if !get {
					log.Warn("multipart lock is held by another process, skipping merge",
						zap.String("bucket", info.BucketName),
						zap.String("objectKey", info.ObjectKey),
						zap.String("uploadID", info.UploadID))
					return
				}

				defer func() {
					if err := locker.ReleaseLock(taskCtx, resourcekey, workerID); err != nil {
						log.Error("timer.handlerTask fail to release multipart lock", zap.Error(err), zap.String("resourceKey", resourcekey))
					}
				}()

				// 处理物理合并任务
				if info.Status == consts.MultipartUploadStatusMergedPhysical {
					_ = updateTaskStatus(taskCtx, taskRepo, task.ID, consts.TaskStatusCompleted, "")
					return
				}

				obj, err := fileRepo.GetByKey(taskCtx, info.BucketName, info.ObjectKey, info.VersionID)
				if err != nil {
					if errors.Is(err, repoerr.ErrNotFound) {
						_ = updateTaskStatus(taskCtx, taskRepo, task.ID, consts.TaskStatusFailed, "object already deleted")
						return
					}
					log.Error("timer.handlerTask fail to get object for physical merge", zap.Error(err), zap.Int64("taskID", taskID))
					return
				}
				if obj.IsMultipart == consts.ObjectIsMultipartNormal && obj.StoragePath != nil {
					physicalStatus := int32(consts.MultipartUploadStatusMergedPhysical)
					if _, err := multipart.UpdateMultipartUpload(taskCtx, info.UserID, info.UploadID, &do.UpdateMultipartUpload{Status: &physicalStatus}); err != nil {
						log.Error("timer.handlerTask fail to update already merged upload status",
							zap.Error(err),
							zap.Int64("taskID", taskID),
							zap.String("uploadID", info.UploadID))
						return
					}
					_ = updateTaskStatus(taskCtx, taskRepo, task.ID, consts.TaskStatusCompleted, "")
					return
				}
				if obj.IsMultipart != consts.ObjectIsMultipartMerged || obj.UploadID == nil || *obj.UploadID != uploadID {
					_ = updateTaskStatus(taskCtx, taskRepo, task.ID, consts.TaskStatusFailed, "object already deleted")
					return
				}

				parts, err := multipart.ListMultipartParts(taskCtx, task.UserId, uploadID)
				if err != nil {
					log.Error("timer.handlerTask ListMultipartParts error", zap.Error(err), zap.Int64("taskID", taskID))
					return
				}

				if info.TotalChunk > 0 && len(parts) != int(info.TotalChunk) {
					err := fmt.Errorf("parts count not match total_chunk: got=%d want=%d", len(parts), info.TotalChunk)
					log.Error("timer.handlerTask physical merge parts count mismatch",
						zap.Error(err),
						zap.Int64("taskID", taskID),
					)
					writeCtx, cancel := context.WithTimeout(taskCtx, 5*time.Second)
					_ = updateTaskStatus(writeCtx, taskRepo, task.ID, consts.TaskStatusFailed, err.Error())
					cancel()
					return
				}

				partPaths := lo.Map(parts, func(part *do.MultipartPartDo, _ int) storage.PartInfo {
					return storage.PartInfo{
						PartNumber: part.PartNumber,
						ETag:       part.Etag,
					}
				})

				saveInfo, err := storageImp.CompleteMultipartUpload(taskCtx, uploadID, partPaths)
				if err != nil {
					writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					log.Error("timer.handlerTask fail to merge parts", zap.Error(err), zap.String("task", gconv.String(task)))
					_ = updateTaskStatus(writeCtx, taskRepo, task.ID, consts.TaskStatusFailed, err.Error())
					return
				}

				status := int32(consts.ObjectIsMultipartNormal)
				physicalStatus := int32(consts.MultipartUploadStatusMergedPhysical)
				err = txManager.RunInTx(taskCtx, func(ctx context.Context, tx tx.Tx) error {
					fileTxRepo := fileRepo.WithTx(tx)
					multipartTxRepo := multipart.WithTx(tx)

					_, err = fileTxRepo.UpdateObject(ctx, info.BucketName, info.ObjectKey, info.VersionID, &do.UpdateObject{
						Size:        &saveInfo.Size,
						Etag:        &saveInfo.Etag,
						StoragePath: &saveInfo.StoragePath,
						IsMultipart: &(status),
					})

					if err != nil {
						log.Error("timer.handlerTask UpdateObject parts", zap.Error(err), zap.String("task", gconv.String(task)))
						return err
					}
					// 清理 multipart 相关数据
					if _, err := multipartTxRepo.UpdateMultipartUpload(ctx, task.UserId, uploadID, &do.UpdateMultipartUpload{Status: &physicalStatus}); err != nil {
						log.Error("timer.handlerTask UpdateMultipartUpload physical status error", zap.Error(err), zap.Int64("taskID", taskID))
						return err
					}

					err = multipartTxRepo.DeleteMultipartParts(ctx, task.UserId, uploadID)
					if err != nil {
						log.Error("timer.handlerTask DeleteMultipartParts error", zap.Error(err), zap.Int64("taskID", taskID))
						return err
					}

					return nil
				})
				if err != nil {
					log.Error("timer.handlerTask runInTx failed", zap.Error(err), zap.Int64("taskId", gconv.Int64(task.ID)))
					writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					if saveInfo.StoragePath != "" {
						if delErr := storageImp.Delete(context.Background(), saveInfo.StoragePath); delErr != nil {
							log.Error("timer.handlerTask cleanup merged file failed",
								zap.Error(delErr),
								zap.String("storagePath", saveInfo.StoragePath),
								zap.Int64("taskID", taskID),
							)
						}
					}

					_ = updateTaskStatus(writeCtx, taskRepo, task.ID, consts.TaskStatusFailed, err.Error())
					cancel()
					return
				}

				// 本地删除
				if adaptor.GetConfig().Storage.Type == "local" {
					err = storageImp.AbortUpload(ctx, info.UploadID)
					if err != nil {
						log.Error("timer.handlerTask AbortUpload error", zap.Error(err), zap.Int64("taskID", taskID))
					}
				}

				writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				if err := updateTaskStatus(writeCtx, taskRepo, task.ID, consts.TaskStatusCompleted, ""); err != nil {
					log.Error("timer.handlerTask update physical merge task completed failed",
						zap.Error(err),
						zap.Int64("taskID", taskID),
					)
				}
				cancel()

				mergedObj, objErr := fileRepo.GetByKey(taskCtx, info.BucketName, info.ObjectKey, info.VersionID)
				if objErr != nil {
					log.Error("timer.handlerTask load merged object for transcode failed",
						zap.Error(objErr),
						zap.String("bucket", info.BucketName),
						zap.String("objectKey", info.ObjectKey),
						zap.String("versionID", info.VersionID),
						zap.Int64("taskID", taskID))
					return
				}
				contentType := ""
				if mergedObj.ContentType != nil {
					contentType = *mergedObj.ContentType
				}

				if err := videoScheduler.ScheduleTranscode(taskCtx, &videoSvc.TranscodeSource{
					UserID:        task.UserId,
					BucketID:      mergedObj.BucketID,
					BucketName:    mergedObj.BucketName,
					ObjectID:      mergedObj.ID,
					ObjectKey:     mergedObj.ObjectKey,
					ObjectKeyHash: mergedObj.ObjectKeyHash,
					VersionID:     mergedObj.VersionID,
					SourceEtag:    saveInfo.Etag,
					SourceSize:    saveInfo.Size,
					ContentType:   contentType,
					SourcePath:    saveInfo.StoragePath,
				}); err != nil {
					log.Warn("timer.handlerTask schedule transcode after physical merge failed",
						zap.Error(err),
						zap.String("bucket", info.BucketName),
						zap.String("objectKey", info.ObjectKey),
						zap.String("versionID", info.VersionID),
						zap.Int64("taskID", taskID))
				}

			case consts.TaskTypePhysicalCopy:
				str := task.BizID
				// BizID:    fmt.Sprintf("%v:%v:%v:%v", bucket, objectKey, source.VersionID, newVersionID),

				ids := strings.SplitN(str, "::", 4)
				if len(ids) != 4 {
					updateTaskStatus(ctx, taskRepo, taskID, consts.TaskStatusFailed, "content error")
					return
				}
				bucket := ids[0]     // bucket
				objectKey := ids[1]  // objectKey
				sVersionID := ids[2] // oldversion
				tVersionID := ids[3] // newversion

				sObj, err := objRepo.GetByKey(ctx, bucket, objectKey, sVersionID)
				if err != nil {
					if err == repoerr.ErrNotFound {
						updateTaskStatus(ctx, taskRepo, taskID, consts.TaskStatusFailed, "source obj not found")
						return
					}
					updateTaskStatus(ctx, taskRepo, taskID, consts.TaskStatusFailed, err.Error())
					return
				}

				// 等待合并
				if sObj.IsMultipart == consts.ObjectIsMultipartMerged {
					updateTaskStatus(ctx, taskRepo, taskID, consts.TaskStatusPending, "")
					return
				}

				_, err = objRepo.GetByKey(ctx, bucket, objectKey, tVersionID)
				if err != nil {
					if err == repoerr.ErrNotFound {
						updateTaskStatus(ctx, taskRepo, taskID, consts.TaskStatusFailed, "target obj not found")
						return
					}
					updateTaskStatus(ctx, taskRepo, taskID, consts.TaskStatusFailed, err.Error())
					return
				}

				reader, err := storageImp.Copy(ctx, *sObj.StoragePath, bucket, objectKey, tVersionID)
				if err != nil {
					updateTaskStatus(ctx, taskRepo, taskID, consts.TaskStatusFailed, err.Error())
					return
				}

				_, err = objRepo.UpdateObject(ctx, bucket, objectKey, tVersionID, &do.UpdateObject{
					StoragePath: &reader.StoragePath,
					Etag:        &reader.Etag,
					Size:        &reader.Size,
				})

				if err != nil {
					updateTaskStatus(ctx, taskRepo, taskID, consts.TaskStatusFailed, err.Error())
					return
				}

				writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				if _, err := taskRepo.CompleteAsyncTask(writeCtx, task.ID, ""); err != nil {
					log.Error("timer.handlerTask update transcode task completed failed",
						zap.Error(err),
						zap.Int64("taskID", taskID))
				}

			case consts.TaskTypeTranscode:
				//转码
				result, err := videoProcessor.HandleTask(taskCtx, task)
				writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err != nil {
					log.Error("timer.handlerTask transcode task failed",
						zap.Error(err),
						zap.Int64("taskID", taskID),
						zap.String("bizID", task.BizID))
					if statusErr := updateTaskStatus(writeCtx, taskRepo, task.ID, consts.TaskStatusFailed, err.Error()); statusErr != nil {
						log.Error("timer.handlerTask update transcode task failed status failed",
							zap.Error(statusErr),
							zap.Int64("taskID", taskID))
					}
					return
				}
				if _, err := taskRepo.CompleteAsyncTask(writeCtx, task.ID, result); err != nil {
					log.Error("timer.handlerTask update transcode task completed failed",
						zap.Error(err),
						zap.Int64("taskID", taskID))
				}

			case consts.TaskTypeAbortMultipart:
				// 放弃合并
				if uploadID == "" {
					_ = updateTaskStatus(taskCtx, taskRepo, task.ID, consts.TaskStatusFailed, "task biz_id is empty")
					return
				}
				info, err := multipart.GetMultipartUploadByID(ctx, task.UserId, uploadID)
				if err != nil {
					log.Error("timer.handlerTask fail to get multipart upload info", zap.Error(err), zap.Int64("taskID", taskID))
					return
				}
				resourcekey := buildLockKey(consts.ServerName, "multipart", info.BucketName, info.ObjectKey)

				get, err := locker.AcquireLock(ctx, resourcekey, workerID, time.Minute*10)
				if err != nil {
					log.Error("timer.handlerTask fail to acquire multipart lock", zap.Error(err), zap.String("resourceKey", resourcekey))
					return
				}

				if !get {
					log.Warn("multipart lock is held by another process, skipping abort",
						zap.String("bucket", info.BucketName),
						zap.String("objectKey", info.ObjectKey),
						zap.String("uploadID", info.UploadID))
					return
				}
				defer func() {
					if err := locker.ReleaseLock(ctx, resourcekey, workerID); err != nil {
						log.Error("timer.handlerTask fail to release multipart lock", zap.Error(err), zap.String("resourceKey", resourcekey))
					}
				}()

				parts, err := multipart.ListMultipartParts(ctx, info.UserID, info.UploadID)
				if err != nil {
					log.Error("timer.handlerTask ListMultipartParts error", zap.Error(err), zap.Int64("taskID", taskID))
					return
				}

				var total int64

				lo.ForEach(parts, func(part *do.MultipartPartDo, _ int) {
					total += int64(part.Size)
				})

				err = txManager.RunInTx(ctx, func(ctx context.Context, tx tx.Tx) error {
					err := multipart.WithTx(tx).DeleteMultipartParts(ctx, task.UserId, uploadID)
					if err != nil {
						log.Error("timer.handlerTask DeleteMultipartParts error", zap.Error(err), zap.Int64("taskID", taskID))
						return err
					}

					err = uinfoRepo.WithTx(tx).UpdateStorageUsed(ctx, info.UserID, -(total))
					if err != nil {
						log.Error("timer.handlerTask UpdateStorageUsed error", zap.Error(err), zap.Int64("taskID", taskID))
						return err
					}

					err = bucketRepo.WithTx(tx).UpdateBucketStats(ctx, info.UserID, info.BucketName, 0, -(total))
					if err != nil {
						log.Error("timer.handlerTask UpdateBucketStats error", zap.Error(err), zap.Int64("taskID", taskID))
						return err
					}

					return nil

				})
				if err != nil {
					log.Error("timer.handlerTask runInTx failed", zap.Error(err), zap.Int64("taskId", gconv.Int64(task.ID)))
					writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					_ = updateTaskStatus(writeCtx, taskRepo, task.ID, consts.TaskStatusFailed, err.Error())
					cancel()
					return
				}

				if err := storageImp.AbortUpload(taskCtx, info.UploadID); err != nil {
					log.Error("timer.handlerTask AbortUpload error",
						zap.Error(err),
						zap.Int64("taskID", taskID),
					)
					writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					_ = updateTaskStatus(writeCtx, taskRepo, task.ID, consts.TaskStatusFailed, err.Error())
					cancel()
					return
				}

				writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				if err := updateTaskStatus(writeCtx, taskRepo, task.ID, consts.TaskStatusCompleted, ""); err != nil {
					log.Error("timer.handlerTask update abort task completed failed",
						zap.Error(err),
						zap.Int64("taskID", taskID),
					)
				}
				cancel()

			default:
				_ = updateTaskStatus(taskCtx, taskRepo, task.ID, consts.TaskStatusFailed, "unknown async task type")
			}

		}); err != nil {
			log.Error("failed to submit task to pool", zap.Error(err))
		}
	}

	p.Wait()
}
