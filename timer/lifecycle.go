package timer

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"time"

	"oss/adaptor"
	"oss/adaptor/redis"
	"oss/adaptor/repo/admin"
	gormAdmin "oss/adaptor/repo/admin/gorm"
	"oss/adaptor/repo/bucket"
	gormBucket "oss/adaptor/repo/bucket/gorm"
	gormLifecycle "oss/adaptor/repo/lifecycle/gorm"
	"oss/adaptor/repo/object"
	gormObject "oss/adaptor/repo/object/gorm"
	"oss/adaptor/storage"
	"oss/adaptor/tx"
	"oss/consts"
	"oss/service/do"
	"oss/utils/pool"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func handlerLifecycleEvents(ctx context.Context, adaptor adaptor.IAdaptor) {
	lifecycleRedis := redis.NewLifecycle(adaptor)
	lifecycleRepo := gormLifecycle.NewLifecycleRepo(adaptor.GetGORM())
	objectRepo := gormObject.NewObjectRepo(adaptor)
	bucketRepo := gormBucket.NewBucketRepo(adaptor)
	storage := adaptor.GetStorage()
	uinfoRepo := gormAdmin.NewUserRepo(adaptor.GetGORM())
	txManager := adaptor.GetTxManager()

	var currentId int64 = 0
	batchSize := 100

	for {
		rules, err := lifecycleRepo.ListAllActiveLifecycleRulesByCursor(ctx, currentId, batchSize)
		if err != nil {
			log.Error("failed to list active lifecycle rules", zap.Error(err))
			return
		}

		poolSize := getLifecyclePoolSize()
		pool := pool.NewPoolWithSize(poolSize)

		for _, rule := range rules {
			rule := rule

			if err := pool.RunGo(func() {
				bucket, err := bucketRepo.GetByID(ctx, rule.BucketID)
				if err != nil {
					log.Error("failed to get bucket for lifecycle rule", zap.Int64("bucketID", rule.BucketID), zap.Int64("ruleID", rule.ID), zap.Error(err))
					return
				}

				handleTransitionEvents(ctx, adaptor, rule, bucket, lifecycleRedis, objectRepo)
				handleExpirationEvents(ctx, adaptor, rule, bucket, lifecycleRedis, objectRepo, bucketRepo, uinfoRepo, txManager, storage)

			}); err != nil {
				log.Error("failed to submit lifecycle handler task to pool", zap.Error(err))
			}
		}
		pool.Wait()
		currentId = rules[len(rules)-1].ID
		if len(rules) < batchSize {
			break // 最后一批
		}
	}

}

func getLifecyclePoolSize() int {
	if size := os.Getenv("LIFECYCLE_POOL_SIZE"); size != "" {
		if parsed, err := strconv.Atoi(size); err == nil && parsed > 0 {
			return parsed
		}
	}
	return runtime.NumCPU() * 2
}

func getRulePrefix(rule *do.LifecycleRuleDo) string {
	if rule.Prefix != nil {
		return *rule.Prefix
	}
	return ""
}

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
		obj, err := objectRepo.GetByKey(ctx, bucket.Name, objectKey, "")
		if err != nil || obj == nil {
			log.Warn("timer.handleTransitionEvents object not found, removing lifecycle event",
				zap.String("bucket", bucket.Name),
				zap.String("objectKey", objectKey),
				zap.Int64("bucketID", rule.BucketID),
				zap.Int64("ruleID", rule.ID))
			lifecycleRedis.DelLifecycleEvent(ctx, rule.BucketID, rule.ID, prefix, "transition", objectKey)
			continue
		}

		if rule.TransitionStorageClass != nil && consts.ValidStorageClass(*rule.TransitionStorageClass) {
			err = txManager.RunInTx(ctx, func(ctx context.Context, tx tx.Tx) error {
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

// 文件失效处理
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
				zap.String("objectKey", objectKey))
			continue
		}

		defer func() {
			err := locker.ReleaseLock(ctx, lockKey, currentWorkId)
			if err != nil {
				log.Error("timer.handleExpirationEvents failed to release lock",
					zap.Error(err),
					zap.String("lockKey", lockKey))
			}
		}()

		cancelCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		go func() {
			ticker := time.NewTicker(time.Second * 10)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					err := locker.RefreshLock(ctx, lockKey, currentWorkId, time.Second*30)
					if err != nil {
						cancel()
					}
				}
			}
		}()
		var eventDeleted bool

		err = txManager.RunInTx(cancelCtx, func(ctx context.Context, tx tx.Tx) error {
			obj, err := objectRepo.WithTx(tx).GetByKey(ctx, bucket.Name, objectKey, "")
			if err != nil || obj == nil {
				log.Warn("timer.handleExpirationEvents object not found in transaction, removing lifecycle event",
					zap.String("bucket", bucket.Name),
					zap.String("objectKey", objectKey),
					zap.Int64("bucketID", rule.BucketID),
					zap.Int64("ruleID", rule.ID))
				lifecycleRedis.DelLifecycleEvent(ctx, rule.BucketID, rule.ID, prefix, "expiration", objectKey)
				eventDeleted = true
				return nil
			}

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

			err = uinfoRepo.WithTx(tx).UpdateStorageUsed(ctx, bucket.UserID, -obj.Size)
			if err != nil {
				log.Error("timer.handleExpirationEvents failed to update user storage used",
					zap.Error(err),
					zap.Int64("userId", bucket.UserID),
					zap.Int64("userId", bucket.UserID))
				return err
			}

			switch obj.IsMultipart {
			case consts.ObjectIsMultipartMerged:
				if obj.UploadID == nil {
					log.Error("multipart object missing upload_id", zap.Int64("obj_id", obj.ID))
					return err
				}
				err = storage.DeleteParts(ctx, obj.BucketName, *obj.UploadID)
			case consts.ObjectIsMultipartNormal:
				if obj.StoragePath == nil {
					log.Error("normal object missing storage_path", zap.Int64("obj_id", obj.ID))
					return err
				}
				err = storage.Delete(ctx, *obj.StoragePath)
			}
			return err
		})

		if !eventDeleted {
			lifecycleRedis.DelLifecycleEvent(ctx, rule.BucketID, rule.ID, prefix, "expiration", objectKey)
		}
	}
}
