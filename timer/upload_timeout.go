package timer

import (
	"context"
	"time"

	"oss/adaptor"
	"oss/adaptor/redis"
	gormAdmin "oss/adaptor/repo/admin/gorm"
	gormMultipart "oss/adaptor/repo/multipart/gorm"
	"oss/adaptor/tx"
	"oss/consts"
	"oss/service/do"
	"oss/utils/pool"

	"github.com/google/uuid"
	"github.com/samber/lo"
	"go.uber.org/zap"
)

func handlerUploadMergeTimeout(ctx context.Context, adaptor adaptor.IAdaptor) {
	multipartRedis := redis.NewMultipart(adaptor)
	multipartRepo := gormMultipart.NewObjectRepo(adaptor.GetGORM())
	storage := adaptor.GetStorage()
	uinfoRepo := gormAdmin.NewUserRepo(adaptor.GetGORM())
	txManager := adaptor.GetTxManager()
	locker := redis.NewLock(adaptor)

	list, err := multipartRedis.GetTimeWaitMultipartCancel(ctx)
	if err != nil {
		return
	}

	pool := pool.NewPoolWithSize(2)

	for _, item := range list {
		item := item
		if err := pool.RunGo(func() {
			lockKey := buildLockKey(consts.ServerName, "merge", "timeout", item)
			currentUUid := uuid.NewString()
			ok, err := locker.AcquireLock(ctx, lockKey, currentUUid, time.Second*30)
			if err != nil {
				log.Error("timer.handlerUploadMergeTimeout fail to acquire lock", zap.Error(err), zap.String("lockKey", lockKey))
				return
			}
			if !ok {
				log.Warn("merge timeout lock is held by another process, skipping", zap.String("uploadID", item))
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
				ticker := time.NewTicker(time.Second * 10)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						if err := locker.RefreshLock(ctx, lockKey, currentUUid, time.Second*30); err != nil {
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

			err = txManager.RunInTx(refreshLockCtx, func(ctx context.Context, tx tx.Tx) error {
				err := uinfoRepo.UpdateStorageUsed(ctx, uploadInfo.UserID, -int64(total))
				if err != nil {
					log.Error("timer.handlerUploadMergeTimeout fail to update user storage used", zap.Error(err), zap.String("uploadID", uploadInfo.UploadID))
					return err
				}

				err = multipartRepo.DeleteMultipartParts(ctx, uploadInfo.UserID, uploadInfo.UploadID)
				if err != nil {
					log.Error("timer.handlerUploadMergeTimeout fail to delete multipart parts", zap.Error(err), zap.String("uploadID", uploadInfo.UploadID))
					return err
				}

				err = storage.DeleteParts(ctx, uploadInfo.BucketName, uploadInfo.UploadID)
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
