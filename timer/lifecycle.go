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
	"oss/adaptor/repo/repoerr"
	"oss/adaptor/storage"
	"oss/adaptor/tx"
	"oss/consts"
	"oss/service/do"
	videoSvc "oss/service/video"
	"oss/utils/pool"
	"oss/utils/tools"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func handlerLifecycleEvents(ctx context.Context, adaptor adaptor.IAdaptor) {
	lifecycleRedis := redis.NewLifecycle(adaptor)
	lifecycleRepo := gormLifecycle.NewLifecycleRepo(adaptor.GetGORM())
	objectRepo := gormObject.NewObjectRepo(adaptor)
	bucketRepo := gormBucket.NewBucketRepo(adaptor)
	storage := adaptor.GetStorage()
	uinfoRepo := gormAdmin.NewUserRepo(adaptor)
	txManager := adaptor.GetTxManager()
	videoCleanup := videoSvc.NewCleanupService(adaptor)

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
				// TODO 可以优化下，批量获取bucket 然后调用map 去映射

				bucket, err := bucketRepo.GetByID(ctx, rule.BucketID)
				if err != nil {
					if err == repoerr.ErrNotFound {
						return
					}

					log.Error("failed to get bucket for lifecycle rule", zap.Int64("bucketID", rule.BucketID), zap.Int64("ruleID", rule.ID), zap.Error(err))
					return
				}
				if err != nil {
					log.Error("failed to get bucket for lifecycle rule", zap.Int64("bucketID", rule.BucketID), zap.Int64("ruleID", rule.ID), zap.Error(err))
					return
				}

				if bucket == nil {
					return
				}

				handleTransitionEvents(ctx, adaptor, rule, bucket, lifecycleRedis, objectRepo)
				handleExpirationEvents(ctx, adaptor, rule, bucket, lifecycleRedis, objectRepo, bucketRepo, uinfoRepo, txManager, storage, videoCleanup)

			}); err != nil {
				log.Error("failed to submit lifecycle handler task to pool", zap.Error(err))
			}
		}
		pool.Wait()
		if len(rules) < batchSize {
			break // 最后一批
		}
		currentId = rules[len(rules)-1].ID
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
	uinfoRepo admin.IUser, txManager tx.ITxManager, storage storage.IStorage, videoCleanup *videoSvc.CleanupService) {

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
		var expiredObj *do.ObjectDo
		var videoCleanupPlan *videoSvc.ObjectVersionCleanup
		var shouldDeleteStorage bool

		err = txManager.RunInTx(cancelCtx, func(ctx context.Context, tx tx.Tx) error {
			objRepo := objectRepo.WithTx(tx)
			obj, err := objRepo.GetByKey(ctx, bucket.Name, objectKey, "")
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

			if obj.Status != consts.ObjectStatusNormal {
				lifecycleRedis.DelLifecycleEvent(ctx, rule.BucketID, rule.ID, prefix, "expiration", objectKey)
				eventDeleted = true
				return nil
			}

			if bucket.Versioning == consts.BucketVersioningEnabled {
				// Lifecycle expiration on a versioned bucket creates a delete marker.
				// It must not delete historical HLS assets, but current-version play tokens
				// should be invalidated after the marker is committed.
				expiredObj = obj
				if err := objRepo.MarkAllNotLatest(ctx, bucket.Name, objectKey); err != nil {
					return err
				}
				if _, err := objRepo.CreateDeleteMarker(ctx, &do.CreateDeleteMarker{
					BucketID:      bucket.ID,
					BucketName:    bucket.Name,
					ObjectKey:     obj.ObjectKey,
					ObjectKeyHash: obj.ObjectKeyHash,
					VersionID:     tools.UUIDHex(),
					StorageClass:  obj.StorageClass,
					Acl:           obj.Acl,
				}); err != nil {
					return err
				}
				return bucketRepo.WithTx(tx).UpdateBucketStats(ctx, bucket.UserID, bucket.Name, -1, 0)
			}

			videoCleanupPlan, err = videoCleanup.PlanObjectVersionCleanup(ctx, obj)
			if err != nil {
				log.Error("timer.handleExpirationEvents failed to plan video cleanup",
					zap.String("bucket", bucket.Name),
					zap.String("objectKey", objectKey),
					zap.String("versionID", obj.VersionID),
					zap.Error(err))
				return err
			}

			expiredObj, err = objRepo.MarkVersionPurged(ctx, bucket.Name, objectKey, obj.VersionID)
			if err != nil {
				log.Error("timer.handleExpirationEvents failed to purge object",
					zap.String("bucket", bucket.Name),
					zap.String("objectKey", objectKey),
					zap.Int64("bucketID", rule.BucketID),
					zap.Int64("ruleID", rule.ID),
					zap.Error(err))
				return err
			}
			if err := videoCleanup.MarkDeletedInTx(ctx, tx, videoCleanupPlan); err != nil {
				log.Error("timer.handleExpirationEvents failed to mark video deleted",
					zap.String("bucket", bucket.Name),
					zap.String("objectKey", objectKey),
					zap.String("versionID", obj.VersionID),
					zap.Error(err))
				return err
			}

			shouldDeleteStorage = true
			deltaSize := -obj.Size
			if videoCleanupPlan != nil && videoCleanupPlan.DerivedSize > 0 {
				deltaSize -= videoCleanupPlan.DerivedSize
			}

			if err = bucketRepo.WithTx(tx).UpdateBucketStats(ctx, bucket.UserID, bucket.Name, -1, deltaSize); err != nil {
				log.Error("timer.handleExpirationEvents failed to update bucket stats",
					zap.String("bucket", bucket.Name),
					zap.String("objectKey", objectKey),
					zap.Int64("bucketID", rule.BucketID),
					zap.Int64("ruleID", rule.ID),
					zap.Error(err))
				return err
			}

			if err = uinfoRepo.WithTx(tx).UpdateStorageUsed(ctx, bucket.UserID, deltaSize); err != nil {
				log.Error("timer.handleExpirationEvents failed to update user storage used",
					zap.Error(err),
					zap.Int64("userId", bucket.UserID),
					zap.Int64("userId", bucket.UserID))
				return err
			}

			return nil
		})

		if !eventDeleted {
			lifecycleRedis.DelLifecycleEvent(ctx, rule.BucketID, rule.ID, prefix, "expiration", objectKey)
		}
		if err == nil {
			if videoCleanupPlan != nil {
				videoCleanup.AfterCommit(ctx, videoCleanupPlan)
			} else if bucket.Versioning == consts.BucketVersioningEnabled && expiredObj != nil {
				videoCleanup.InvalidateObjectVersionTokens(ctx, expiredObj)
			}
		}
		if err == nil && shouldDeleteStorage && expiredObj != nil {
			switch expiredObj.IsMultipart {
			case consts.ObjectIsMultipartMerged:
				if expiredObj.UploadID != nil {
					if deleteErr := storage.DeleteParts(ctx, expiredObj.BucketName, *expiredObj.UploadID); deleteErr != nil {
						log.Error("timer.handleExpirationEvents failed to delete multipart storage",
							zap.String("bucket", expiredObj.BucketName),
							zap.String("uploadID", *expiredObj.UploadID),
							zap.Error(deleteErr))
					}
				}
			default:
				if expiredObj.StoragePath != nil {
					if deleteErr := storage.Delete(ctx, *expiredObj.StoragePath); deleteErr != nil {
						log.Error("timer.handleExpirationEvents failed to delete object storage",
							zap.String("storagePath", *expiredObj.StoragePath),
							zap.Error(deleteErr))
					}
				}
			}
		}
	}
}
