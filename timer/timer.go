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
	"oss/adaptor/repo/async"
	"oss/adaptor/repo/bucket"
	eventRepo "oss/adaptor/repo/event"
	"oss/adaptor/repo/lifecycle"
	"oss/adaptor/repo/multipart"
	"oss/adaptor/repo/object"
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
	taskRepo := async.NewAsyncTaskRepo(adaptor.GetGORM())
	storage := adaptor.GetStorage()
	multipart := multipart.NewObjectRepo(adaptor.GetGORM())
	fileRepo := object.NewObjectRepo(adaptor.GetGORM())
	taskLocker := redis.NewLock(adaptor)
	uinfoRepo := admin.NewUserRepo(adaptor.GetGORM())
	taskIDs, err := redisTask.DequeueTask(ctx, 50, time.Second*5)

	if err != nil {
		// 处理错误（如日志记录、监控告警等）
		return
	}

	p := pool.NewPoolWithSize(5)

	for _, taskID := range taskIDs {
		taskID := taskID // 每次迭代创建新变量

		if err := p.RunGo(func() {
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
	multipartRepo := multipart.NewObjectRepo(adaptor.GetGORM())
	storage := adaptor.GetStorage()
	uinfoRepo := admin.NewUserRepo(adaptor.GetGORM())

	list, err := multipartRedis.GetTimeWaitMultipartCancel(ctx)
	if err != nil {
		// 处理错误（如日志记录、监控告警等）
		return
	}

	pool := pool.NewPoolWithSize(2)

	for _, item := range list {
		item := item // 每次迭代创建新变量，避免闭包捕获问题
		if err := pool.RunGo(func() {

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
	lifecycleRepo := lifecycle.NewLifecycleRepo(adaptor.GetGORM())
	objectRepo := object.NewObjectRepo(adaptor.GetGORM())
	bucketRepo := bucket.NewBucketRepo(adaptor.GetGORM())
	storage := adaptor.GetStorage()
	uinfoRepo := admin.NewUserRepo(adaptor.GetGORM())

	// 使用共享的 gorm DB 实例
	gormDB := adaptor.GetGORM()

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
				// 处理错误
				return
			}

			// 处理转换事件
			handleTransitionEvents(ctx, adaptor, rule, bucket, lifecycleRedis, objectRepo)

			// 处理过期删除事件
			handleExpirationEvents(ctx, adaptor, rule, bucket, lifecycleRedis, objectRepo, bucketRepo, uinfoRepo, gormDB, storage)
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
		var storagePath *string
		var objectSize int64
		var eventDeleted bool

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
				eventDeleted = true
				return nil // 不算错误，继续处理
			}

			storagePath = obj.StoragePath
			objectSize = obj.Size

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

		// 删除物理文件信息在事务外执行，避免文件系统操作影响事务
		if storagePath != nil {
			if err := storage.Delete(ctx, *storagePath); err != nil {
				log.Error("failed to delete physical file",
					zap.String("bucket", bucket.Name),
					zap.String("objectKey", objectKey),
					zap.String("storagePath", *storagePath),
					zap.Error(err))
			} else {
				log.Info("successfully deleted expired object",
					zap.String("bucket", bucket.Name),
					zap.String("objectKey", objectKey),
					zap.Int64("size", objectSize))
			}
		}

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
	eventDeliveryRepo := eventRepo.NewEventDeliveryRepo(adaptor.GetGORM())
	eventRuleRepo := eventRepo.NewEventRuleRepo(adaptor.GetGORM())
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
