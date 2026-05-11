package timer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"oss/adaptor"
	"oss/adaptor/redis"
	"oss/adaptor/repo/admin"
	gormAdmin "oss/adaptor/repo/admin/gorm"
	"oss/adaptor/repo/async"
	gormAsync "oss/adaptor/repo/async/gorm"
	"oss/adaptor/repo/bucket"
	gormLifecycle "oss/adaptor/repo/lifecycle/gorm"
	"oss/adaptor/tx"

	// "oss/adaptor/repo/multipart"
	gormBucket "oss/adaptor/repo/bucket/gorm"
	gormEvent "oss/adaptor/repo/event/gorm"
	gormMultipart "oss/adaptor/repo/multipart/gorm"
	"oss/adaptor/repo/object"
	gormObject "oss/adaptor/repo/object/gorm"
	"oss/adaptor/storage"
	"oss/consts"
	"oss/service/do"
	"oss/utils/logger"
	"oss/utils/pool"
	"oss/utils/tools"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gogf/gf/util/gconv"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"go.uber.org/zap"
)

var log = logger.GetLogger()

func updateTaskStatus(ctx context.Context, taskRepo async.IAsyncTaskRepo, taskID int64, status int32, errMsg string) error {
	update := &do.UpdateAsyncTask{Status: status}
	if errMsg != "" {
		update.ErrorMsg = errMsg
	}
	_, err := taskRepo.UpdateAsyncTask(ctx, taskID, update)
	return err
}

func buildLockKey(keys ...string) string {
	return strings.Join(keys, ":")
}

func handlerTask(ctx context.Context, adaptor adaptor.IAdaptor) {
	// 从 Redis 队列中取出任务 ID
	redisTask := redis.NewTask(adaptor)
	taskRepo := gormAsync.NewAsyncTaskRepo(adaptor.GetGORM())
	storage := adaptor.GetStorage()
	multipart := gormMultipart.NewObjectRepo(adaptor.GetGORM())
	fileRepo := gormObject.NewObjectRepo(adaptor.GetGORM())
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
				txManager.RunInTx(ctx, func(tx tx.Tx) error {
					err := updateTaskStatus(taskCtx, taskRepo, task.ID, consts.TaskStatusCompleted, "")
					if err != nil {
						log.Error("timer.handlerTask updateTaskStatus parts", zap.Error(err), zap.String("task", gconv.String(task)))
						return err
					}

					_, err = fileRepo.UpdateObject(taskCtx, info.BucketName, info.ObjectKey, "", &do.UpdateObject{
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
					err = multipart.DeleteMultipartParts(taskCtx, task.UserId, task.UploadID)
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

			case consts.TaskTypeAbortMultipart:
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
				parts, err := multipart.ListMultipartParts(taskCtx, info.UserID, info.UploadID)
				if err != nil {
					log.Error("timer.handlerTask ListMultipartParts error", zap.Error(err), zap.String("taskID", taskID))
					return
				}

				total := 0

				lo.ForEach(parts, func(part *do.MultipartPartDo, _ int) {
					total += int(part.Size)
				})

				txManager.RunInTx(ctx, func(tx tx.Tx) error {
					err := multipart.DeleteMultipartParts(taskCtx, task.UserId, task.UploadID)
					if err != nil {
						log.Error("timer.handlerTask DeleteMultipartParts error", zap.Error(err), zap.String("taskID", taskID))
						return err
					}

					err = uinfoRepo.UpdateStorageUsed(taskCtx, info.UserID, -int64(total))
					if err != nil {
						log.Error("timer.handlerTask UpdateStorageUsed error", zap.Error(err), zap.String("taskID", taskID))
						return err
					}

					// 清理 multipart 相关数据
					err = storage.DeleteParts(taskCtx, info.BucketName, task.UploadID)
					if err != nil {
						log.Error("timer.handlerTask DeleteParts error", zap.Error(err), zap.String("taskID", taskID))
						return err
					}
					return nil
				})

			default:
				// 处理未知任务类型
			}

		}); err != nil {
			log.Error("failed to submit task to pool", zap.Error(err))
		}
	}

	p.Wait()
}

// GetTimeWaitMultipartCancel 获取等待取消的 multipart 上传 ID 列表
func handlerUploadMergeTimeout(ctx context.Context, adaptor adaptor.IAdaptor) {
	// 处理上传超时的任务
	multipartRedis := redis.NewMultipart(adaptor)
	multipartRepo := gormMultipart.NewObjectRepo(adaptor.GetGORM())
	storage := adaptor.GetStorage()
	uinfoRepo := gormAdmin.NewUserRepo(adaptor.GetGORM())
	txManager := adaptor.GetTxManager()
	locker := redis.NewLock(adaptor)

	list, err := multipartRedis.GetTimeWaitMultipartCancel(ctx)
	if err != nil {
		// 处理错误（如日志记录、监控告警等）
		return
	}

	pool := pool.NewPoolWithSize(2)

	for _, item := range list {
		item := item // 每次迭代创建新变量，避免闭包捕获问题
		if err := pool.RunGo(func() {

			lockKey := buildLockKey(consts.ServerName, "merge", "timeout", item)
			currentUUid := uuid.NewString()
			ok, err := locker.AcquireLock(ctx, lockKey, currentUUid, time.Second*30)
			if err != nil {
				log.Error("timer.handlerUploadMergeTimeout fail to acquire lock", zap.Error(err), zap.String("lockKey", lockKey))
				return
			}
			if !ok {
				log.Warn("merge timeout lock is held by another process, skipping",
					zap.String("uploadID", item))
				return
			}
			defer func() {
				if err := locker.ReleaseLock(ctx, lockKey, currentUUid); err != nil {
					log.Error("timer.handlerUploadMergeTimeout fail to release lock", zap.Error(err), zap.String("lockKey", lockKey))
				}
			}()

			refreshLockCtx, cancel := context.WithCancel(ctx)
			defer cancel()

			go func() {

				ticker := time.NewTicker(time.Second * 10) // 每 10 秒续期一次
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						if err := locker.RefreshLock(ctx, lockKey, currentUUid, time.Second*30); err != nil {
							// 续期失败说明锁已丢失，应中断任务
							cancel()
							return
						}
					}
				}

			}()

			uploadInfo, err := multipartRepo.GetMultipartUploadByID(refreshLockCtx, 0, item)
			if err != nil {
				log.Error("timer.handlerUploadMergeTimeout fail to get multipart upload info", zap.Error(err), zap.String("uploadID", item))
				return
			}

			// 只有文件已经虚拟合并，证明全部校验完毕，不做任何处理
			if uploadInfo.Status == consts.MultipartPartStatusVirtualMerge {
				return
			}

			total := 0
			parts, err := multipartRepo.ListMultipartParts(refreshLockCtx, uploadInfo.UserID, uploadInfo.UploadID)
			if err != nil {
				log.Error("timer.handlerUploadMergeTimeout fail to list multipart parts", zap.Error(err), zap.String("uploadID", uploadInfo.UploadID))
				return
			}

			lo.ForEach(parts, func(item *do.MultipartPartDo, _ int) {
				total += int(item.Size)
			})
			txManager.RunInTx(refreshLockCtx, func(tx tx.Tx) error {
				// 用户状态修正
				err := uinfoRepo.UpdateStorageUsed(refreshLockCtx, uploadInfo.UserID, -int64(total))
				if err != nil {
					log.Error("timer.handlerUploadMergeTimeout fail to update user storage used", zap.Error(err), zap.String("uploadID", uploadInfo.UploadID))
					return err
				}

				// 清楚系统文件
				err = multipartRepo.DeleteMultipartParts(refreshLockCtx, uploadInfo.UserID, uploadInfo.UploadID)
				if err != nil {
					log.Error("timer.handlerUploadMergeTimeout fail to delete multipart parts", zap.Error(err), zap.String("uploadID", uploadInfo.UploadID))
					return err
				}
				// 存储删除
				err = storage.DeleteParts(refreshLockCtx, uploadInfo.BucketName, uploadInfo.UploadID)
				if err != nil {
					log.Error("timer.handlerUploadMergeTimeout fail to delete parts", zap.Error(err), zap.String("uploadID", uploadInfo.UploadID))
					return err
				}

				return nil
			})

		}); err != nil {
			log.Error("failed to submit task to pool", zap.Error(err))
		}

	}
}

// handlerLifecycleEvents 处理生命周期事件的主函数
// 该函数负责处理对象的存储类转换和过期删除操作
// 使用协程池并发处理多个生命周期规则，提高处理效率
func handlerLifecycleEvents(ctx context.Context, adaptor adaptor.IAdaptor) {
	lifecycleRedis := redis.NewLifecycle(adaptor)
	lifecycleRepo := gormLifecycle.NewLifecycleRepo(adaptor.GetGORM())
	objectRepo := gormObject.NewObjectRepo(adaptor.GetGORM())
	bucketRepo := gormBucket.NewBucketRepo(adaptor.GetGORM())
	storage := adaptor.GetStorage()
	uinfoRepo := gormAdmin.NewUserRepo(adaptor.GetGORM())
	txManager := adaptor.GetTxManager()

	// 获取所有活跃的生命周期规则
	rules, err := lifecycleRepo.ListAllActiveLifecycleRules(ctx)
	if err != nil {
		log.Error("failed to list active lifecycle rules", zap.Error(err))
		return
	}

	// 动态协程池大小，默认使用CPU核心数
	poolSize := getLifecyclePoolSize()
	pool := pool.NewPoolWithSize(poolSize)

	for _, rule := range rules {
		rule := rule // 避免闭包捕获问题

		if err := pool.RunGo(func() {
			bucket, err := bucketRepo.GetByID(ctx, rule.BucketID)
			if err != nil {
				log.Error("failed to get bucket for lifecycle rule", zap.Int64("bucketID", rule.BucketID), zap.Int64("ruleID", rule.ID), zap.Error(err))
				return
			}

			// 处理转换事件
			handleTransitionEvents(ctx, adaptor, rule, bucket, lifecycleRedis, objectRepo)

			// 处理过期删除事件
			handleExpirationEvents(ctx, adaptor, rule, bucket, lifecycleRedis, objectRepo, bucketRepo, uinfoRepo, txManager, storage)

		}); err != nil {
			log.Error("failed to submit lifecycle handler task to pool", zap.Error(err))
		}
	}

	pool.Wait()
}

// getLifecyclePoolSize 获取生命周期处理协程池大小
func getLifecyclePoolSize() int {
	if size := os.Getenv("LIFECYCLE_POOL_SIZE"); size != "" {
		if parsed, err := strconv.Atoi(size); err == nil && parsed > 0 {
			return parsed
		}
	}
	// 默认使用CPU核心数的2倍
	return runtime.NumCPU() * 2
}

// getRulePrefix 获取规则的前缀
func getRulePrefix(rule *do.LifecycleRuleDo) string {
	if rule.Prefix != nil {
		return *rule.Prefix
	}
	return ""
}

// handleTransitionEvents 处理存储类转换事件
func handleTransitionEvents(ctx context.Context, adaptor adaptor.IAdaptor, rule *do.LifecycleRuleDo, bucket *do.BucketDo,
	lifecycleRedis redis.ILifecycle, objectRepo object.IObjectRepo) {

	if rule.TransitionDays == nil || *rule.TransitionDays <= 0 {
		return
	}

	prefix := getRulePrefix(rule)
	objects, err := lifecycleRedis.GetPendingLifecycleEvents(ctx, rule.BucketID, rule.ID, prefix, "transition")
	if err != nil {
		log.Error("timer.handleTransitionEvents failed to get pending transition events",
			zap.Int64("bucketID", rule.BucketID),
			zap.Int64("ruleID", rule.ID),
			zap.String("prefix", prefix),
			zap.Error(err))
		return
	}
	txManager := adaptor.GetTxManager()
	for _, objectKey := range objects {
		// 检查对象是否仍然存在
		obj, err := objectRepo.GetByKey(ctx, bucket.Name, objectKey, "")
		if err != nil || obj == nil {
			// 对象不存在，删除事件
			log.Warn("timer.handleTransitionEvents object not found, removing lifecycle event",
				zap.String("bucket", bucket.Name),
				zap.String("objectKey", objectKey),
				zap.Int64("bucketID", rule.BucketID),
				zap.Int64("ruleID", rule.ID))
			lifecycleRedis.DelLifecycleEvent(ctx, rule.BucketID, rule.ID, prefix, "transition", objectKey)
			continue
		}

		// 验证存储类有效性
		if rule.TransitionStorageClass != nil && consts.ValidStorageClass(*rule.TransitionStorageClass) {
			txManager.RunInTx(ctx, func(tx tx.Tx) error {
				err = objectRepo.UpdateObjectStorageClass(ctx, bucket.Name, objectKey, *rule.TransitionStorageClass)
				if err != nil {
					log.Error("timer.handleTransitionEvents failed to update object storage class",
						zap.String("bucket", bucket.Name),
						zap.String("objectKey", objectKey),
						zap.String("storageClass", *rule.TransitionStorageClass),
						zap.Error(err))
					return err
				}
				log.Info("timer.handleTransitionEvents successfully transitioned object storage class",
					zap.String("bucket", bucket.Name),
					zap.String("objectKey", objectKey),
					zap.String("fromClass", obj.StorageClass),
					zap.String("toClass", *rule.TransitionStorageClass))

				// 删除已处理的事件
				err = lifecycleRedis.DelLifecycleEvent(ctx, rule.BucketID, rule.ID, prefix, "transition", objectKey)
				if err != nil {
					log.Error("timer.handleTransitionEvents failed to delete lifecycle event",
						zap.String("bucket", bucket.Name),
						zap.String("objectKey", objectKey),
						zap.Int64("bucketID", rule.BucketID),
						zap.Int64("ruleID", rule.ID),
						zap.Error(err))
				}

				return nil
			})
		}
	}
}

// handleExpirationEvents 处理对象过期删除事件
func handleExpirationEvents(ctx context.Context, adaptor adaptor.IAdaptor, rule *do.LifecycleRuleDo, bucket *do.BucketDo,
	lifecycleRedis redis.ILifecycle, objectRepo object.IObjectRepo, bucketRepo bucket.IBucketRepo,
	uinfoRepo admin.IUser, txManager tx.ITxManager, storage storage.IStorage) {

	locker := redis.NewLock(adaptor)
	if rule.ExpirationDays == nil || *rule.ExpirationDays <= 0 {
		return
	}

	prefix := getRulePrefix(rule)
	objects, err := lifecycleRedis.GetPendingLifecycleEvents(ctx, rule.BucketID, rule.ID, prefix, "expiration")
	if err != nil {
		log.Error("timer.handleExpirationEvents failed to get pending expiration events",
			zap.Int64("bucketID", rule.BucketID),
			zap.Int64("ruleID", rule.ID),
			zap.String("prefix", prefix),
			zap.Error(err))
		return
	}

	for _, objectKey := range objects {

		currentWorkId := uuid.NewString()
		lockKey := buildLockKey(consts.ServerName, "event", "expire", objectKey)

		pass, err := locker.AcquireLock(ctx, lockKey, currentWorkId, time.Second*30)
		if err != nil {
			log.Error("timer.handleExpirationEvents failed to acquire lock",
				zap.String("bucket", bucket.Name),
				zap.String("objectKey", objectKey),
				zap.Int64("bucketID", rule.BucketID),
				zap.Int64("ruleID", rule.ID),
				zap.Error(err))
			continue
		}

		if !pass {
			log.Warn("timer.handleExpirationEvents lock is held by another process, skipping",
				zap.String("bucket", bucket.Name),
				zap.String("objectKey", objectKey),
			)
			continue
		}

		defer func() {
			err := locker.ReleaseLock(ctx, lockKey, currentWorkId)
			if err != nil {
				log.Error("timer.handleExpirationEvents failed to release lock",
					zap.Error(err),
					zap.String("lockKey", lockKey),
				)
			}
		}()

		cancelCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		go func() {
			ticker := time.NewTicker(time.Second * 10) // 每 10 秒续期一次
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return

				case <-ticker.C:
					err := locker.RefreshLock(ctx, lockKey, currentWorkId, time.Second*30)
					if err != nil {
						cancel() // 续期失败说明锁已丢失，应中断任务
					}
				}
			}
		}()
		var eventDeleted bool

		// 在事务内部处理删除，避免竞态条件
		err = txManager.RunInTx(cancelCtx, func(tx tx.Tx) error {
			// 在事务内部重新检查对象是否存在

			obj, err := objectRepo.WithTx(tx).GetByKey(ctx, bucket.Name, objectKey, "")
			if err != nil || obj == nil {
				// 对象不存在，删除事件
				log.Warn("timer.handleExpirationEvents object not found in transaction, removing lifecycle event",
					zap.String("bucket", bucket.Name),
					zap.String("objectKey", objectKey),
					zap.Int64("bucketID", rule.BucketID),
					zap.Int64("ruleID", rule.ID))
				lifecycleRedis.DelLifecycleEvent(ctx, rule.BucketID, rule.ID, prefix, "expiration", objectKey)
				eventDeleted = true
				return nil // 不算错误，继续处理
			}

			// 删除对象记录
			err = objectRepo.WithTx(tx).DeleteObject(ctx, bucket.Name, objectKey, "")
			if err != nil {
				log.Error("timer.handleExpirationEvents failed to delete object",
					zap.String("bucket", bucket.Name),
					zap.String("objectKey", objectKey),
					zap.Int64("bucketID", rule.BucketID),
					zap.Int64("ruleID", rule.ID),
					zap.Error(err))
				return err
			}

			// 更新bucket统计
			err = bucketRepo.WithTx(tx).UpdateBucketStats(ctx, bucket.UserID, bucket.Name, -1, -obj.Size)
			if err != nil {
				log.Error("timer.handleExpirationEvents failed to update bucket stats",
					zap.String("bucket", bucket.Name),
					zap.String("objectKey", objectKey),
					zap.Int64("bucketID", rule.BucketID),
					zap.Int64("ruleID", rule.ID),
					zap.Error(err))
				return err
			}

			// 更新用户存储使用量
			err = uinfoRepo.WithTx(tx).UpdateStorageUsed(ctx, bucket.UserID, -obj.Size)
			if err != nil {
				log.Error("timer.handleExpirationEvents failed to update user storage used",
					zap.Error(err),
					zap.Int64("userId", (bucket.UserID)),
					zap.Int64("userId", (bucket.UserID)),
				)
				return err
			}

			switch obj.IsMultipart {
			case consts.ObjectIsMultipartMerged:
				if obj.UploadID == nil {
					logger.Error("multipart object missing upload_id", zap.Int64("obj_id", obj.ID))
					return err
				}
				err = storage.DeleteParts(ctx, obj.BucketName, *obj.UploadID)

			case consts.ObjectIsMultipartNormal:
				if obj.StoragePath == nil {
					logger.Error("normal object missing storage_path", zap.Int64("obj_id", obj.ID))
					return err
				}
				err = storage.Delete(ctx, *obj.StoragePath)
			}
			return err
		})

		// 删除已处理的事件
		if !eventDeleted {
			lifecycleRedis.DelLifecycleEvent(ctx, rule.BucketID, rule.ID, prefix, "expiration", objectKey)
		}
	}
}

// 定时器相关的代码
const (
	taskInterval               = 30 * time.Second
	uploadMergeTimeoutInterval = 30 * time.Second
	lifecycleInterval          = 1 * time.Minute
	eventDeliveryInterval      = 10 * time.Second
)

func runTimerTask(ctx context.Context, name string, done chan struct{}, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error("timer handler panic",
					zap.String("handler", name),
					zap.Any("panic", r),
					zap.Stack("stack"))
			}
			select {
			case done <- struct{}{}:
			default:
			}
		}()

		fn()
	}()
}

func startTimerTaskLoop(ctx context.Context, name string, interval time.Duration, done chan struct{}, fn func()) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// 尝试第一次立即执行
		select {
		case <-done:
			runTimerTask(ctx, name, done, fn)
		default:
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				select {
				case <-done:
					runTimerTask(ctx, name, done, fn)
				default:
					// 上一轮还没完成，跳过
				}
			}
		}
	}()
}

func StartTimer(ctx context.Context, adaptor adaptor.IAdaptor) {
	taskDone := make(chan struct{}, 1)
	timeoutDone := make(chan struct{}, 1)
	lifecycleDone := make(chan struct{}, 1)
	eventDone := make(chan struct{}, 1)
	taskDone <- struct{}{}
	timeoutDone <- struct{}{}
	lifecycleDone <- struct{}{}
	eventDone <- struct{}{}

	startTimerTaskLoop(ctx, "handlerTask", taskInterval, taskDone, func() {
		handlerTask(ctx, adaptor)
	})

	startTimerTaskLoop(ctx, "handlerUploadMergeTimeout", uploadMergeTimeoutInterval, timeoutDone, func() {
		handlerUploadMergeTimeout(ctx, adaptor)
	})
	startTimerTaskLoop(ctx, "handlerLifecycleEvents", lifecycleInterval, lifecycleDone, func() {
		handlerLifecycleEvents(ctx, adaptor)
	})
	startTimerTaskLoop(ctx, "handlerEventDeliveries", eventDeliveryInterval, eventDone, func() {
		handlerEventDeliveries(ctx, adaptor)
	})

	<-ctx.Done()
}

// handlerEventDeliveries 处理事件投递任务
func handlerEventDeliveries(ctx context.Context, adaptor adaptor.IAdaptor) {
	eventDeliveryRepo := gormEvent.NewEventDeliveryRepo(adaptor.GetGORM())
	eventRuleRepo := gormEvent.NewEventRuleRepo(adaptor.GetGORM())
	eventQueue := redis.NewEventQueue(adaptor)

	// 尝试从 Redis 触发队列取出 delivery_id
	deliveryIDs, err := eventQueue.DequeueDeliveryIDs(ctx, 50, time.Second*5)
	if err != nil {
		log.Error("failed to dequeue event delivery IDs", zap.Error(err))
		return
	}

	if len(deliveryIDs) == 0 {
		// 如果触发队列为空，检查数据库中待处理或到期重试的记录，补齐触发队列
		deliveries, err := eventDeliveryRepo.GetPendingDeliveries(ctx, 50)
		if err != nil {
			log.Error("failed to scan pending event deliveries", zap.Error(err))
			return
		}

		for _, delivery := range deliveries {
			if delivery.Status == consts.EventDeliveryStatusFailed {
				pending := int32(consts.EventDeliveryStatusPending)
				if err := eventDeliveryRepo.UpdateEventDelivery(ctx, delivery.ID, &do.UpdateEventDelivery{
					Status:      &pending,
					NextRetryAt: nil,
				}); err != nil {
					log.Error("failed to reset failed delivery to pending", zap.Int64("deliveryID", delivery.ID), zap.Error(err))
					continue
				}
			}

			if err := eventQueue.EnqueueDeliveryID(ctx, delivery.ID); err != nil {
				log.Error("failed to enqueue pending delivery ID", zap.Int64("deliveryID", delivery.ID), zap.Error(err))
			}
		}
		return
	}

	for _, deliveryID := range deliveryIDs {
		delivery, err := eventDeliveryRepo.GetEventDeliveryByID(ctx, deliveryID)
		if err != nil {
			log.Error("failed to get event delivery by ID", zap.Int64("deliveryID", deliveryID), zap.Error(err))
			continue
		}
		if delivery == nil {
			log.Warn("event delivery record not found", zap.Int64("deliveryID", deliveryID))
			continue
		}

		if delivery.Status != consts.EventDeliveryStatusPending {
			log.Warn("skipping non-pending delivery", zap.Int64("deliveryID", delivery.ID), zap.Int32("status", delivery.Status))
			continue
		}

		// 获取事件规则
		rule, err := eventRuleRepo.GetByID(ctx, delivery.RuleID)
		if err != nil || rule == nil {
			log.Error("failed to get event rule", zap.Int64("ruleID", delivery.RuleID), zap.Error(err))
			continue
		}

		// 执行投递
		err = deliverEvent(ctx, adaptor, rule, delivery)
		update := &do.UpdateEventDelivery{
			Status: &[]int32{consts.EventDeliveryStatusSuccess}[0],
		}

		if err != nil {
			log.Error("failed to deliver event",
				zap.Int64("deliveryID", delivery.ID),
				zap.String("eventType", delivery.EventType),
				zap.Error(err))

			// 更新重试信息
			retryCount := delivery.RetryCount + 1
			update.Status = &[]int32{consts.EventDeliveryStatusFailed}[0]
			update.RetryCount = &retryCount

			// 如果重试次数未达到上限，设置下次重试时间
			if retryCount < 3 {
				nextRetry := time.Now().Add(time.Duration(retryCount) * time.Minute)
				update.NextRetryAt = &nextRetry
			}
		}

		// 更新投递状态
		if updateErr := eventDeliveryRepo.UpdateEventDelivery(ctx, delivery.ID, update); updateErr != nil {
			log.Error("failed to update event delivery status",
				zap.Int64("deliveryID", delivery.ID), zap.Error(updateErr))
		}
	}
}

// deliverEvent 执行具体的事件投递
func deliverEvent(ctx context.Context, adaptor adaptor.IAdaptor, rule *do.EventRuleDo, delivery *do.EventDeliveryDo) error {
	switch rule.TargetType {
	case consts.EventTargetTypeWebhook:
		return deliverWebhook(ctx, rule, delivery)
	case consts.EventTargetTypeMQ, consts.EventTargetTypeRedis:
		return deliverRedis(ctx, adaptor, delivery)

	default:
		return fmt.Errorf("unsupported target type: %s", rule.TargetType)
	}
}

func deliverRedis(ctx context.Context, adaptor adaptor.IAdaptor, delivery *do.EventDeliveryDo) error {
	payload := map[string]interface{}{
		"event_type": delivery.EventType,
		"object_key": delivery.ObjectKey,
		"payload":    delivery.Payload,
		"timestamp":  time.Now().Unix(),
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("oss:event:%d:deliveries", delivery.RuleID)
	if err := adaptor.GetRedis().RPush(ctx, key, string(bodyBytes)).Err(); err != nil {
		return err
	}

	log.Info("Redis MQ delivery queued",
		zap.String("queueKey", key),
		zap.Int64("deliveryID", delivery.ID),
		zap.String("eventType", delivery.EventType))

	return nil
}

// deliverWebhook 通过Webhook投递事件
func deliverWebhook(ctx context.Context, rule *do.EventRuleDo, delivery *do.EventDeliveryDo) error {
	if rule.TargetURL == nil || *rule.TargetURL == "" {
		return fmt.Errorf("webhook URL not configured")
	}

	payload := map[string]interface{}{
		"event_type": delivery.EventType,
		"object_key": delivery.ObjectKey,
		"payload":    delivery.Payload,
		"timestamp":  time.Now().Unix(),
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, *rule.TargetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	if rule.Secret != nil && *rule.Secret != "" {
		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		stringToSign := strings.Join([]string{timestamp, string(bodyBytes)}, "\n")
		signature := tools.HmacSHA256(stringToSign, *rule.Secret)
		req.Header.Set("X-Event-Timestamp", timestamp)
		req.Header.Set("X-Event-Signature", signature)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook request failed status=%d response=%s", resp.StatusCode, string(respBody))
	}

	truncatedResponse := string(respBody)
	if len(truncatedResponse) > 200 {
		truncatedResponse = truncatedResponse[:200] + "...(truncated)"
	}

	log.Info("Webhook delivery success",
		zap.String("url", *rule.TargetURL),
		zap.String("eventType", delivery.EventType),
		zap.String("status", resp.Status),
		zap.String("responseBody", truncatedResponse),
		zap.Int("responseBodyLength", len(respBody)))

	return nil
}
