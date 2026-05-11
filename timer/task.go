package timer

import (
	"context"
	"oss/adaptor"
	"oss/adaptor/redis"
	gormAdmin "oss/adaptor/repo/admin/gorm"
	gormAsync "oss/adaptor/repo/async/gorm"
	gormMultipart "oss/adaptor/repo/multipart/gorm"
	gormObject "oss/adaptor/repo/object/gorm"
	"oss/adaptor/tx"
	"oss/consts"
	"oss/service/do"
	"oss/utils/pool"
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
	storage := adaptor.GetStorage()
	multipart := gormMultipart.NewObjectRepo(adaptor.GetGORM())
	fileRepo := gormObject.NewObjectRepo(adaptor)
	taskLocker := redis.NewLock(adaptor)
	uinfoRepo := gormAdmin.NewUserRepo(adaptor.GetGORM())
	txManager := adaptor.GetTxManager()
	taskIDs, err := redisTask.DequeueTask(ctx, 50, time.Second*5)
	locker := redis.NewLock(adaptor)

	if err != nil {
		log.Error("timer fail to dequeue task", zap.Error(err))
		return
	}

	p := pool.NewPoolWithSize(5)

	for _, taskID := range taskIDs {
		taskID := taskID // 每次迭代创建新变量

		if err := p.RunGo(func() {
			currentUUid := uuid.NewString()
			ok, err := taskLocker.AcquireLock(ctx, taskID, currentUUid, time.Second*30)
			if err != nil {
				log.Error("timer.handlerTask fail to acquire lock", zap.Error(err))
				return
			}
			if !ok {
				// 锁被占用，跳过
				return
			}

			defer func() {
				if err := taskLocker.ReleaseLock(ctx, taskID, currentUUid); err != nil {
					log.Error("timer.handlerTask fail to release lock", zap.Error(err))
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
						if err := taskLocker.RefreshLock(ctx, taskID, currentUUid, time.Second*30); err != nil {
							// 续期失败说明锁已丢失，应中断任务
							cancelTask()
							return
						}
					}
				}
			}()

			// 根据 taskID 查询数据库获取任务详情
			task, err := taskRepo.GetAsyncTaskByID(taskCtx, gconv.Int64(taskID))
			if err != nil {
				log.Error("timer.handlerTask fail to get async task", zap.Error(err), zap.String("taskID", taskID))
				return
			}

			switch task.TaskType {
			case consts.TaskTypePhysicalMerge:
				info, err := multipart.GetMultipartUploadByID(taskCtx, task.UserId, task.UploadID)
				if err != nil {
					log.Error("timer.handlerTask fail to get multipart upload info", zap.Error(err), zap.String("taskID", taskID))
					return
				}

				resourcekey := buildLockKey(consts.ServerName, "multipart", info.BucketName, info.ObjectKey)

				get, err := locker.AcquireLock(ctx, resourcekey, currentUUid, time.Minute*10)
				if err != nil {
					log.Error("timer.handlerTask fail to acquire multipart lock", zap.Error(err), zap.String("resourceKey", resourcekey))
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
					if err := locker.ReleaseLock(ctx, resourcekey, currentUUid); err != nil {
						log.Error("timer.handlerTask fail to release multipart lock", zap.Error(err), zap.String("resourceKey", resourcekey))
					}
				}()

				// 处理物理合并任务
				parts, err := multipart.ListMultipartParts(taskCtx, task.UserId, task.UploadID)
				if err != nil {
					log.Error("timer.handlerTask ListMultipartParts error", zap.Error(err), zap.String("taskID", taskID))
					return
				}

				partPaths := make([]string, len(parts))
				for i, part := range parts {
					partPaths[i] = part.StoragePath
				}

				saveInfo, err := storage.MergeParts(taskCtx, info.BucketName, info.ObjectKey, partPaths)
				if err != nil {
					writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					log.Error("timer.handlerTask fail to merge parts", zap.Error(err), zap.String("task", gconv.String(task)))
					_ = updateTaskStatus(writeCtx, taskRepo, task.ID, consts.TaskStatusFailed, err.Error())
					return
				}

				status := int32(consts.ObjectIsMultipartNormal)
				err = txManager.RunInTx(taskCtx, func(ctx context.Context, tx tx.Tx) error {
					err := updateTaskStatus(ctx, taskRepo, task.ID, consts.TaskStatusCompleted, "")
					if err != nil {
						log.Error("timer.handlerTask updateTaskStatus parts", zap.Error(err), zap.String("task", gconv.String(task)))
						return err
					}

					_, err = fileRepo.UpdateObject(ctx, info.BucketName, info.ObjectKey, "", &do.UpdateObject{
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
					err = multipart.DeleteMultipartParts(ctx, task.UserId, task.UploadID)
					if err != nil {
						log.Error("timer.handlerTask DeleteMultipartParts error", zap.Error(err), zap.String("taskID", taskID))
						return err
					}
					err = storage.DeleteParts(ctx, info.BucketName, info.UploadID)
					if err != nil {
						log.Error("timer.handlerTask DeleteParts error", zap.Error(err), zap.String("taskID", taskID))
						return err
					}

					return nil
				})
				if err != nil {
					log.Error("timer.handlerTask runInTx failed", zap.Error(err), zap.Int64("taskId", gconv.Int64(task.ID)))
					return
				}

			case consts.TaskTypeAbortMultipart:
				info, err := multipart.GetMultipartUploadByID(ctx, task.UserId, task.UploadID)
				if err != nil {
					log.Error("timer.handlerTask fail to get multipart upload info", zap.Error(err), zap.String("taskID", taskID))
					return
				}
				resourcekey := buildLockKey(consts.ServerName, "multipart", info.BucketName, info.ObjectKey)

				get, err := locker.AcquireLock(ctx, resourcekey, currentUUid, time.Minute*10)
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
					if err := locker.ReleaseLock(ctx, resourcekey, currentUUid); err != nil {
						log.Error("timer.handlerTask fail to release multipart lock", zap.Error(err), zap.String("resourceKey", resourcekey))
					}
				}()
				parts, err := multipart.ListMultipartParts(ctx, info.UserID, info.UploadID)
				if err != nil {
					log.Error("timer.handlerTask ListMultipartParts error", zap.Error(err), zap.String("taskID", taskID))
					return
				}

				total := 0

				lo.ForEach(parts, func(part *do.MultipartPartDo, _ int) {
					total += int(part.Size)
				})

				err = txManager.RunInTx(ctx, func(ctx context.Context, tx tx.Tx) error {
					err := multipart.DeleteMultipartParts(ctx, task.UserId, task.UploadID)
					if err != nil {
						log.Error("timer.handlerTask DeleteMultipartParts error", zap.Error(err), zap.String("taskID", taskID))
						return err
					}

					err = uinfoRepo.UpdateStorageUsed(ctx, info.UserID, -int64(total))
					if err != nil {
						log.Error("timer.handlerTask UpdateStorageUsed error", zap.Error(err), zap.String("taskID", taskID))
						return err
					}

					// 清理 multipart 相关数据
					err = storage.DeleteParts(ctx, info.BucketName, task.UploadID)
					if err != nil {
						log.Error("timer.handlerTask DeleteParts error", zap.Error(err), zap.String("taskID", taskID))
						return err
					}

					return nil

				})
				if err != nil {
					log.Error("timer.handlerTask runInTx failed", zap.Error(err), zap.Int64("taskId", gconv.Int64(task.ID)))
				}

			default:
				// 处理未知任务类型
			}

		}); err != nil {
			log.Error("failed to submit task to pool", zap.Error(err))
		}
	}

	p.Wait()
}
