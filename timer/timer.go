package timer

import (
	"context"
	"os"
	"oss/adaptor"
	"oss/adaptor/redis"
	"oss/adaptor/repo/admin"
	"oss/adaptor/repo/async"
	"oss/adaptor/repo/bucket"
	"oss/adaptor/repo/lifecycle"
	"oss/adaptor/repo/multipart"
	"oss/adaptor/repo/object"
	"oss/adaptor/storage"
	"oss/consts"
	"oss/service/do"
	"oss/utils/logger"
	"oss/utils/pool"
	"runtime"
	"strconv"
	"time"

	"github.com/gogf/gf/util/gconv"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var log = logger.GetLogger()

func updateTaskStatus(ctx context.Context, taskRepo async.IAsyncTaskRepo, taskID int64, status int32, errMsg string) {
	update := &do.UpdateAsyncTask{Status: status}
	if errMsg != "" {
		update.ErrorMsg = errMsg
	}
	_, _ = taskRepo.UpdateAsyncTask(ctx, taskID, update)
}

func handlerTask(ctx context.Context, adaptor adaptor.IAdaptor) {
	// 从 Redis 队列中取出任务 ID
	redisTask := redis.NewTask(adaptor)
	taskRepo := async.NewAsyncTaskRepo(adaptor)
	storage := adaptor.GetStorage()
	multipart := multipart.NewObjectRepo(adaptor)
	fileRepo := object.NewObjectRepo(adaptor)
	taskLocker := redis.NewLock(adaptor)
	uinfoRepo := admin.NewUserRepo(adaptor)
	taskIDs, err := redisTask.DequeueTask(ctx, 50, time.Second*5)

	if err != nil {
		// 处理错误（如日志记录、监控告警等）
		return
	}

	p := pool.NewPoolWithSize(5)

	for _, taskID := range taskIDs {
		taskID := taskID // 每次迭代创建新变量

		p.RunGo(func() {
			currentUUid := uuid.NewString()

			ok, err := taskLocker.AcquireLock(ctx, taskID, currentUUid, time.Second*30)
			if err != nil {
				// 处理错误
				return
			}
			if !ok {
				// 锁被占用，跳过
				return
			}

			defer func() {
				if err := taskLocker.ReleaseLock(ctx, taskID, currentUUid); err != nil {
					// 处理释放锁时的错误（如日志记录、监控告警等）
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
				// 处理错误（如日志记录、监控告警等）
				return
			}

			switch task.TaskType {
			case consts.TaskTypePhysicalMerge:
				info, err := multipart.GetMultipartUploadByID(taskCtx, task.UserId, task.UploadID)
				if err != nil {
					return
				}

				// 处理物理合并任务
				parts, err := multipart.ListMultipartParts(taskCtx, task.UserId, task.UploadID)
				if err != nil {
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
					updateTaskStatus(writeCtx, taskRepo, task.ID, consts.TaskStatusFailed, err.Error())
					return
				}

				updateTaskStatus(taskCtx, taskRepo, task.ID, consts.TaskStatusCompleted, "")
				fileRepo.UpdateObject(taskCtx, info.BucketName, info.ObjectKey, "", &do.UpdateObject{
					Size:        &saveInfo.Size,
					Etag:        &saveInfo.Etag,
					StoragePath: &saveInfo.StoragePath,
				})

			case consts.TaskTypeAbortMultipart:
				info, err := multipart.GetMultipartUploadByID(taskCtx, task.UserId, task.UploadID)
				if err != nil {
					return
				}

				parts, err := multipart.ListMultipartParts(taskCtx, info.UserID, info.UploadID)
				if err != nil {
					return
				}

				total := 0

				lo.ForEach(parts, func(part *do.MultipartPartDo, _ int) {
					total += int(part.Size)
				})

				multipart.DeleteMultipartParts(taskCtx, task.UserId, task.UploadID)
				uinfoRepo.UpdateStorageUsed(taskCtx, info.UserID, -int64(total))

				storage.DeleteParts(taskCtx, info.BucketName, task.UploadID)

			default:
				// 处理未知任务类型
			}

		})
	}

	p.Wait()
}

// GetTimeWaitMultipartCancel 获取等待取消的 multipart 上传 ID 列表
func handlerUploadMergeTimeout(ctx context.Context, adaptor adaptor.IAdaptor) {
	// 处理上传超时的任务
	multipartRedis := redis.NewMultipart(adaptor)
	multipartRepo := multipart.NewObjectRepo(adaptor)
	storage := adaptor.GetStorage()
	uinfoRepo := admin.NewUserRepo(adaptor)

	list, err := multipartRedis.GetTimeWaitMultipartCancel(ctx)
	if err != nil {
		// 处理错误（如日志记录、监控告警等）
		return
	}

	pool := pool.NewPoolWithSize(2)

	for _, item := range list {
		_ = item // 每次迭代创建新变量，避免闭包捕获问题
		pool.RunGo(func() {

			uploadInfo, err := multipartRepo.GetMultipartUploadByID(ctx, 0, item)
			if err != nil {
				// 处理错误
				return
			}

			// 只有文件已经虚拟合并，证明全部校验完毕，不做任何处理
			if uploadInfo.Status == consts.MultipartPartStatusVirtualMerge {
				return
			}

			total := 0
			parts, err := multipartRepo.ListMultipartParts(ctx, uploadInfo.UserID, uploadInfo.UploadID)
			if err != nil {
				// 处理错误
				return
			}

			lo.ForEach(parts, func(item *do.MultipartPartDo, _ int) {
				total += int(item.Size)
			})

			// 本次存储删除
			storage.DeleteParts(ctx, uploadInfo.BucketName, uploadInfo.UploadID)
			// 用户状态修正
			uinfoRepo.UpdateStorageUsed(ctx, uploadInfo.UserID, -int64(total))
			// 清楚系统文件
			multipartRepo.DeleteMultipartParts(ctx, uploadInfo.UserID, uploadInfo.UploadID)

		})

	}
}

// handlerLifecycleEvents 处理生命周期事件的主函数
// 该函数负责处理对象的存储类转换和过期删除操作
// 使用协程池并发处理多个生命周期规则，提高处理效率
func handlerLifecycleEvents(ctx context.Context, adaptor adaptor.IAdaptor) {
	lifecycleRedis := redis.NewLifecycle(adaptor)
	lifecycleRepo := lifecycle.NewLifecycleRepo(adaptor)
	objectRepo := object.NewObjectRepo(adaptor)
	bucketRepo := bucket.NewBucketRepo(adaptor)
	storage := adaptor.GetStorage()
	uinfoRepo := admin.NewUserRepo(adaptor)

	// 创建gorm DB实例用于事务
	sqlDB := adaptor.GetDB()
	gormDB, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		log.Error("failed to create gorm DB instance", zap.Error(err))
		return
	}

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

		pool.RunGo(func() {
			bucket, err := bucketRepo.GetByID(ctx, rule.BucketID)
			if err != nil {
				// 处理错误
				return
			}

			// 处理转换事件
			handleTransitionEvents(ctx, adaptor, rule, bucket, lifecycleRedis, objectRepo)

			// 处理过期删除事件
			handleExpirationEvents(ctx, adaptor, rule, bucket, lifecycleRedis, objectRepo, bucketRepo, uinfoRepo, gormDB, storage)
		})
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
		log.Error("failed to get pending transition events",
			zap.Int64("bucketID", rule.BucketID),
			zap.Int64("ruleID", rule.ID),
			zap.String("prefix", prefix),
			zap.Error(err))
		return
	}

	for _, objectKey := range objects {
		// 检查对象是否仍然存在
		obj, err := objectRepo.GetByKey(ctx, bucket.Name, objectKey, "")
		if err != nil || obj == nil {
			// 对象不存在，删除事件
			log.Warn("object not found, removing lifecycle event",
				zap.String("bucket", bucket.Name),
				zap.String("objectKey", objectKey),
				zap.Int64("bucketID", rule.BucketID),
				zap.Int64("ruleID", rule.ID))
			lifecycleRedis.DelLifecycleEvent(ctx, rule.BucketID, rule.ID, prefix, "transition", objectKey)
			continue
		}

		// 验证存储类有效性
		if rule.TransitionStorageClass != nil && consts.ValidStorageClass(*rule.TransitionStorageClass) {
			err = objectRepo.UpdateObjectStorageClass(ctx, bucket.Name, objectKey, *rule.TransitionStorageClass)
			if err != nil {
				log.Error("failed to update object storage class",
					zap.String("bucket", bucket.Name),
					zap.String("objectKey", objectKey),
					zap.String("storageClass", *rule.TransitionStorageClass),
					zap.Error(err))
				continue
			}
			log.Info("successfully transitioned object storage class",
				zap.String("bucket", bucket.Name),
				zap.String("objectKey", objectKey),
				zap.String("fromClass", obj.StorageClass),
				zap.String("toClass", *rule.TransitionStorageClass))
		}

		// 删除已处理的事件
		lifecycleRedis.DelLifecycleEvent(ctx, rule.BucketID, rule.ID, prefix, "transition", objectKey)
	}
}

// handleExpirationEvents 处理对象过期删除事件
func handleExpirationEvents(ctx context.Context, adaptor adaptor.IAdaptor, rule *do.LifecycleRuleDo, bucket *do.BucketDo,
	lifecycleRedis redis.ILifecycle, objectRepo object.IObjectRepo, bucketRepo bucket.IBucketRepo,
	uinfoRepo admin.IUser, gormDB *gorm.DB, storage storage.IStorage) {

	if rule.ExpirationDays == nil || *rule.ExpirationDays <= 0 {
		return
	}

	prefix := getRulePrefix(rule)
	objects, err := lifecycleRedis.GetPendingLifecycleEvents(ctx, rule.BucketID, rule.ID, prefix, "expiration")
	if err != nil {
		log.Error("failed to get pending expiration events",
			zap.Int64("bucketID", rule.BucketID),
			zap.Int64("ruleID", rule.ID),
			zap.String("prefix", prefix),
			zap.Error(err))
		return
	}

	for _, objectKey := range objects {
		// 在事务内部处理删除，避免竞态条件
		err = gormDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// 在事务内部重新检查对象是否存在
			obj, err := objectRepo.GetByKeyWithTx(tx, ctx, bucket.Name, objectKey, "")
			if err != nil || obj == nil {
				// 对象不存在，删除事件
				log.Warn("object not found in transaction, removing lifecycle event",
					zap.String("bucket", bucket.Name),
					zap.String("objectKey", objectKey),
					zap.Int64("bucketID", rule.BucketID),
					zap.Int64("ruleID", rule.ID))
				lifecycleRedis.DelLifecycleEvent(ctx, rule.BucketID, rule.ID, prefix, "expiration", objectKey)
				return nil // 不算错误，继续处理
			}

			// 删除对象记录
			err = objectRepo.DeleteObjectWithTx(tx, ctx, bucket.Name, objectKey, "")
			if err != nil {
				return err
			}

			// 更新bucket统计
			err = bucketRepo.UpdateBucketStatsWithTx(tx, ctx, bucket.UserID, bucket.Name, -1, -obj.Size)
			if err != nil {
				return err
			}

			// 更新用户存储使用量
			err = uinfoRepo.UpdateStorageUsedWithTx(tx, ctx, bucket.UserID, -obj.Size)
			if err != nil {
				return err
			}

			return nil
		})

		if err != nil {
			log.Error("failed to delete expired object",
				zap.String("bucket", bucket.Name),
				zap.String("objectKey", objectKey),
				zap.Int64("bucketID", rule.BucketID),
				zap.Int64("ruleID", rule.ID),
				zap.Error(err))
			continue
		}

		// 获取对象信息用于删除物理文件（在事务外部，因为存储删除不应该在事务中）
		obj, err := objectRepo.GetByKey(ctx, bucket.Name, objectKey, "")
		if err == nil && obj != nil && obj.StoragePath != nil {
			if err := storage.Delete(ctx, *obj.StoragePath); err != nil {
				log.Error("failed to delete physical file",
					zap.String("bucket", bucket.Name),
					zap.String("objectKey", objectKey),
					zap.String("storagePath", *obj.StoragePath),
					zap.Error(err))
			} else {
				log.Info("successfully deleted expired object",
					zap.String("bucket", bucket.Name),
					zap.String("objectKey", objectKey),
					zap.Int64("size", obj.Size))
			}
		}

		// 删除已处理的事件
		lifecycleRedis.DelLifecycleEvent(ctx, rule.BucketID, rule.ID, prefix, "expiration", objectKey)
	}
}

// 定时器相关的代码
func StartTimer(ctx context.Context, adaptor adaptor.IAdaptor) {
	taskDone := make(chan struct{}, 1)
	timeoutDone := make(chan struct{}, 1)
	lifecycleDone := make(chan struct{}, 1)
	taskDone <- struct{}{}
	timeoutDone <- struct{}{}
	lifecycleDone <- struct{}{}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		select {
		case <-taskDone:
			go func() {
				handlerTask(ctx, adaptor)
				taskDone <- struct{}{}
			}()
		default: // 上一轮还没完成，跳过
		}

		select {
		case <-timeoutDone:
			go func() {
				handlerUploadMergeTimeout(ctx, adaptor)
				timeoutDone <- struct{}{}
			}()
		default:
		}

		select {
		case <-lifecycleDone:
			go func() {
				handlerLifecycleEvents(ctx, adaptor)
				lifecycleDone <- struct{}{}
			}()
		default:
		}

		time.Sleep(time.Second * 30)
	}
}
