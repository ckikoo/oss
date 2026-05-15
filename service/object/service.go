package object

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"sort"
	"strings"
	"time"

	"oss/adaptor"
	"oss/adaptor/redis"
	"oss/adaptor/repo/admin"
	gormAdmin "oss/adaptor/repo/admin/gorm"
	"oss/adaptor/repo/bucket"
	gormBucket "oss/adaptor/repo/bucket/gorm"
	eventI "oss/adaptor/repo/event"
	gormEvent "oss/adaptor/repo/event/gorm"
	"oss/adaptor/repo/lifecycle"
	gormLifecycle "oss/adaptor/repo/lifecycle/gorm"
	"oss/adaptor/repo/metering"
	gormMetering "oss/adaptor/repo/metering/gorm"
	Imultipart "oss/adaptor/repo/multipart"
	gormMultipart "oss/adaptor/repo/multipart/gorm"
	"oss/adaptor/repo/object"
	gormObject "oss/adaptor/repo/object/gorm"
	"oss/adaptor/repo/repoerr"
	"oss/adaptor/storage"
	"oss/adaptor/tx"
	"oss/common"
	"oss/consts"
	"oss/service/do"
	"oss/service/dto"
	"oss/service/event"
	"oss/utils/ip"
	"oss/utils/logger"
	"oss/utils/tools"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/gogf/gf/util/gconv"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type Service struct {
	txManger      tx.ITxManager
	adaptor       adaptor.IAdaptor
	userRepo      admin.IUser
	objRepo       object.IObjectRepo
	bucketRepo    bucket.IBucketRepo
	multipartRepo Imultipart.IMultipartRepo
	meteringRepo  metering.IMeteringRepo
	storage       storage.IStorage
	eventService  *event.Service
	locker        redis.ILock
	logger        *zap.Logger
	lifecycleRepo lifecycle.ILifecycleRepo
	lifeRedis     redis.ILifecycle
	eventRepo     eventI.IEventDeliveryRepo
	eventQueue    redis.IEventQueue
}

func NewService(adaptor adaptor.IAdaptor) *Service {
	return &Service{
		txManger:      adaptor.GetTxManager(),
		adaptor:       adaptor,
		userRepo:      gormAdmin.NewUserRepo(adaptor),
		objRepo:       gormObject.NewObjectRepo(adaptor),
		bucketRepo:    gormBucket.NewBucketRepo(adaptor),
		multipartRepo: gormMultipart.NewObjectRepo(adaptor.GetGORM()),
		meteringRepo:  gormMetering.NewMeteringRepo(adaptor.GetGORM()),
		storage:       adaptor.GetStorage(),
		eventService:  event.NewService(adaptor),
		logger:        logger.GetLogger().With(zap.String("module", "object")),
		lifecycleRepo: gormLifecycle.NewLifecycleRepo(adaptor.GetGORM()),
		lifeRedis:     redis.NewLifecycle(adaptor),
		locker:        redis.NewLock(adaptor),
		eventRepo:     gormEvent.NewEventDeliveryRepo(adaptor.GetGORM()),
		eventQueue:    redis.NewEventQueue(adaptor),
	}
}

func (srv *Service) ListObjects(ctx *common.UserInfoCtx, req *dto.ListObjectsReq) (*dto.ListObjectsResp, common.Errno) {
	objects, err := srv.objRepo.ListByFilter(ctx, req.BucketName, req.Prefix, req.Delimiter, req.Marker, req.MaxKeys, req.VersionID)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	items := make([]*dto.ObjectItem, 0, len(objects))
	for _, obj := range objects {
		contentType := ""
		if obj.ContentType != nil {
			contentType = *obj.ContentType
		}
		items = append(items, &dto.ObjectItem{
			ObjectKey:    obj.ObjectKey,
			Size:         obj.Size,
			Etag:         obj.Etag,
			ContentType:  contentType,
			StorageClass: obj.StorageClass,
			VersionID:    obj.VersionID,
			LastModified: obj.UpdatedAt.UnixMilli(),
			Status:       obj.Status,
		})
	}

	return &dto.ListObjectsResp{Items: items}, common.OK
}

func (srv *Service) GetObjectMetadata(ctx *common.UserInfoCtx, bucketName, objectKey, versionID string) (*dto.ObjectMetadata, common.Errno) {
	if bucketName == "" || objectKey == "" {
		return nil, common.ParamErr.WithMsg("bucket_name and object_key are required")
	}

	bucket, err := srv.bucketRepo.GetByName(ctx, ctx.UserID, bucketName)
	if err != nil {
		return nil, common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.BucketNotFoundErr)
	}

	if bucket == nil {
		return nil, common.BucketNotFoundErr
	}

	obj, err := srv.objRepo.GetByKey(ctx, bucketName, objectKey, versionID)
	if err != nil {
		return nil, common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.ResouceNotFoundErr)
	}

	metadata := ""
	if obj.Metadata != nil {
		metadata = *obj.Metadata
	}
	contentType := ""
	if obj.ContentType != nil {
		contentType = *obj.ContentType
	}

	return &dto.ObjectMetadata{
		ObjectKey:    obj.ObjectKey,
		Size:         obj.Size,
		Etag:         obj.Etag,
		ContentType:  contentType,
		StorageClass: obj.StorageClass,
		VersionID:    obj.VersionID,
		Acl:          obj.Acl,
		Metadata:     metadata,
		Status:       obj.Status,
	}, common.OK
}

func (srv *Service) GetObjectVersions(ctx *common.UserInfoCtx, bucketName, objectKey string) (*dto.GetObjectVersionsResp, common.Errno) {
	if bucketName == "" || objectKey == "" {
		return nil, common.ParamErr.WithMsg("bucket_name and object_key are required")
	}

	objects, err := srv.objRepo.ListVersionsByFilter(ctx, bucketName, objectKey)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	items := make([]*dto.ObjectMetadata, 0, len(objects))
	for _, obj := range objects {
		contentType := ""
		if obj.ContentType != nil {
			contentType = *obj.ContentType
		}
		items = append(items, &dto.ObjectMetadata{
			ObjectKey:    obj.ObjectKey,
			Size:         obj.Size,
			Etag:         obj.Etag,
			ContentType:  contentType,
			StorageClass: obj.StorageClass,
			VersionID:    obj.VersionID,
			Status:       obj.Status,
		})
	}

	return &dto.GetObjectVersionsResp{Items: items}, common.OK
}

func (srv *Service) PutObject(ctx *common.UserInfoCtx, req *dto.PutObjectReq, file *multipart.FileHeader) (*dto.PutObjectResp, common.Errno) {
	if req.CallbackUrl != "" {
		if err := ip.ValidateCallbackURL(req.CallbackUrl); err != nil {
			return nil, common.ParamErr.WithErr(err)
		}
	}

	bucket, err := srv.bucketRepo.GetByName(ctx, ctx.UserID, req.BucketName)
	if err != nil {
		return nil, common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.BucketNotFoundErr)
	}

	bucketID := bucket.ID

	if err := srv.meteringRepo.UpdateDailyMetrics(ctx, bucket.UserID, &bucket.ID, time.Now(), 0, 0, file.Size, 0, 1, 0, 0); err != nil {
		srv.logger.Error("object PutObject meteringRepo.UpdateDailyMetrics error",
			zap.Error(err),
			zap.String("req", gconv.String(req)),
			zap.Int64("uploadSize", file.Size),
		)
	}

	objectKeyHash := tools.Md5Hash(req.ObjectKey)

	// Check if object already exists
	cacheFile, err := srv.objRepo.GetByKey(ctx, req.BucketName, req.ObjectKey, "")
	if err != nil && !errors.Is(err, repoerr.ErrNotFound) {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	// Handle versioning
	versionID := tools.UUIDHex()
	if bucket.Versioning == consts.BucketVersioningEnabled { // 版本控制开启，生成新的versionID，保留旧版本
		// Generate UUID for version ID when versioning is enabled
	} else { // 旧的版本

		if req.Overwrite == false && cacheFile != nil {
			return nil, common.FileNameExists
		}

		// If versioning is disabled and object exists, delete the old one
		if cacheFile != nil {
			err := srv.txManger.RunInTx(ctx, func(ctx1 context.Context, tx tx.Tx) error {
				// Mark old object as deleted
				if err := srv.objRepo.WithTx(tx).DeleteObject(ctx1, req.BucketName, req.ObjectKey, cacheFile.VersionID); err != nil {
					srv.logger.Error("PutObject DeleteObject ",
						zap.String("bucket", req.BucketName),
						zap.String("objectKey", req.ObjectKey),
						zap.String("versionID", cacheFile.VersionID),
						zap.Error(err))

					return err
				}

				if err := srv.multipartRepo.WithTx(tx).DeleteMultipartParts(ctx1, ctx.UserID, *cacheFile.UploadID); err != nil {
					srv.logger.Error("PutObject DeleteMultipartParts ",
						zap.String("bucket", req.BucketName),
						zap.String("objectKey", req.ObjectKey),
						zap.String("uploadID", *cacheFile.UploadID),
						zap.Error(err))
					return err
				}

				if err := srv.userRepo.WithTx(tx).UpdateStorageUsed(ctx1, ctx.UserID, -cacheFile.Size); err != nil {
					srv.logger.Error("service.PutObject UpdateStorageUsed",
						zap.String("bucket", req.BucketName),
						zap.String("objectKey", req.ObjectKey),
						zap.String("versionID", cacheFile.VersionID),
						zap.Error(err))
					return err
				}

				return nil
			})

			// Delete old version's storage // 可以删除也可以不删除，后续会被覆盖掉
			if cacheFile.IsMultipart == consts.ObjectIsMultipartMerged {
				if err := srv.storage.DeleteParts(ctx, cacheFile.BucketName, *cacheFile.UploadID); err != nil {
					srv.logger.Error("PutObject  DeleteParts ",
						zap.String("path", *cacheFile.StoragePath),
						zap.Error(err))
				}
			}

			if err != nil {
				return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
			}
		}
	}

	rules, err := srv.lifecycleRepo.ListLifecycleRules(ctx, bucket.ID)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	uInfo, err := srv.userRepo.GetUserInfoById(ctx, ctx.UserID)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	if uInfo.StorageQuota != 0 && uInfo.StorageUsed+file.Size > uInfo.StorageQuota {
		return nil, common.StorageQuotaOver
	}

	// Open file and upload to storage
	f, err := file.Open()
	if err != nil {
		return nil, common.ServerErr.WithErr(err)
	}
	defer f.Close()

	putResult, err := srv.storage.Put(ctx, req.BucketName, req.ObjectKey, versionID, f)
	if err != nil {
		return nil, common.ServerErr.WithErr(err)
	}

	// Default values
	storageClass := req.StorageClass
	if storageClass == "" {
		storageClass = consts.StorageClassStandard
	}
	// Create object
	createObj := &do.CreateObject{
		BucketID:      bucketID,
		BucketName:    req.BucketName,
		ObjectKey:     req.ObjectKey,
		ObjectKeyHash: objectKeyHash,
		VersionID:     versionID,
		Size:          putResult.Size,
		Etag:          putResult.Etag,
		ContentType:   &req.ContentType,
		StorageClass:  storageClass,
		IsMultipart:   consts.ObjectIsMultipartNormal,
		StoragePath:   &putResult.StoragePath,
		Acl:           req.Acl,
		UploadID: func() *string {
			if req.UploadID == "" {
				return nil
			}
			return &req.UploadID
		}(),
		Metadata: func() *string {
			if req.Metadata == "" {
				return nil
			}
			return &req.Metadata
		}(),
		CallBack: func(tx *gorm.DB) error {
			return nil
		},
	}

	var id int64
	err = srv.txManger.RunInTx(ctx, func(ctx1 context.Context, tx tx.Tx) error {
		if err := srv.objRepo.WithTx(tx).UpdateObjectNotLatest(ctx1, createObj.BucketName, createObj.ObjectKey, createObj.VersionID); err != nil {
			return err
		}

		id, err = srv.objRepo.WithTx(tx).CreateObject(ctx1, createObj)
		if err != nil {
			return err
		}

		err = srv.bucketRepo.WithTx(tx).UpdateBucketStats(ctx1, ctx.UserID, req.BucketName, 1, putResult.Size)
		if err != nil {
			return err
		}

		return srv.userRepo.WithTx(tx).UpdateStorageUsed(ctx1, ctx.UserID, putResult.Size)
	})

	if err != nil {
		srv.storage.Delete(ctx, putResult.StoragePath)
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	now := time.Now()
	for _, rule := range rules {
		prefix := ""
		if rule.Prefix != nil {
			prefix = *rule.Prefix
		}

		if strings.HasPrefix(req.ObjectKey, prefix) {
			srv.scheduleObjectEvents(ctx, bucket.ID, rule, prefix, &do.ObjectDo{
				ObjectKey: req.ObjectKey,
				CreatedAt: now,
				VersionID: createObj.VersionID,
			}, now)
		}
	}

	go srv.eventService.TriggerEvent(ctx, bucketID, consts.EventTypePutObject, req.ObjectKey, map[string]interface{}{
		"bucket_name":   req.BucketName,
		"object_key":    req.ObjectKey,
		"size":          putResult.Size,
		"etag":          putResult.Etag,
		"content_type":  req.ContentType,
		"storage_class": storageClass,
	})

	if req.CallbackUrl != "" {

		callbackPayload := map[string]interface{}{
			"callback_url": req.CallbackUrl,
			"event_type":   "multipart_complete",
			"bucket_name":  req.BucketName,
			"object_key":   req.ObjectKey,
			"upload_id":    createObj.UploadID,
			"object_id":    id,
			"version_id":   createObj.VersionID,
			"size":         createObj.Size,
			"etag":         createObj.Etag,
			"status":       "completed",
		}

		srv.dispatchCallback(ctx, req.CallbackUrl, callbackPayload)

	}

	return &dto.PutObjectResp{
		ObjectKey:   req.ObjectKey,
		Size:        putResult.Size,
		Etag:        putResult.Etag,
		StoragePath: putResult.StoragePath,
		VersionID:   versionID, // Return the version ID set during creation
	}, common.OK
}

func (srv *Service) dispatchCallback(ctx context.Context, url string, payload map[string]interface{}) {
	// 1. 写 event_deliveries（rule_id = nil，改表允许 NULL）
	deliveryID, err := srv.eventRepo.CreateEventDelivery(ctx, &do.EventDeliveryDo{
		RuleID:    0,
		EventType: "multipart_complete",
		ObjectKey: &url,
		Payload:   gconv.String(payload),
		Status:    consts.EventDeliveryStatusPending,
	})
	if err != nil {
		srv.logger.Error("failed to create event delivery", zap.Error(err))
		return
	}

	// 2. 入 Redis 队列，timer 接管后续投递和重试
	if err := srv.eventQueue.EnqueueDeliveryID(ctx, deliveryID); err != nil {
		srv.logger.Error("failed to enqueue delivery", zap.Int64("id", deliveryID), zap.Error(err))

	}
}
func (srv *Service) scheduleObjectEvents(
	ctx context.Context,
	bucketID int64,
	rule *do.LifecycleRuleDo,
	prefix string,
	obj *do.ObjectDo,
	now time.Time,
) {
	// rule 被禁用，不入队
	if rule.Status == 0 {
		return
	}

	// transition
	if rule.TransitionDays != nil && *rule.TransitionDays > 0 {
		executeTime := obj.CreatedAt.AddDate(0, 0, int(*rule.TransitionDays))
		if executeTime.After(now) { // 已过期的不入队，直接跳过
			if err := srv.lifeRedis.SetLifecycleEvent(ctx, bucketID, rule.ID, prefix,
				"transition", obj.ObjectKey, executeTime); err != nil {
				srv.logger.Warn("scheduleObjectEvents set transition failed",
					zap.String("objectKey", obj.ObjectKey), zap.Error(err))
			}
		}
	}

	// expiration
	if rule.ExpirationDays != nil && *rule.ExpirationDays > 0 {
		executeTime := obj.CreatedAt.AddDate(0, 0, int(*rule.ExpirationDays))
		if executeTime.After(now) {
			if err := srv.lifeRedis.SetLifecycleEvent(ctx, bucketID, rule.ID, prefix,
				"expiration", obj.ObjectKey, executeTime); err != nil {
				srv.logger.Warn("scheduleObjectEvents set expiration failed",
					zap.String("objectKey", obj.ObjectKey), zap.Error(err))
			}
		}
	}
}
func (srv *Service) GetObject(ctx *common.UserInfoCtx, bucketName, objectKey, versionID string, c *app.RequestContext) common.Errno {
	obj, err := srv.objRepo.GetByKey(ctx, bucketName, objectKey, versionID)
	if err != nil {
		return common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.ResouceNotFoundErr)
	}

	// Set response headers
	contentType := "application/octet-stream"
	if obj.ContentType != nil && *obj.ContentType != "" {
		contentType = *obj.ContentType
	}

	c.Header("Content-Type", contentType)
	c.Header("ETag", obj.Etag)
	c.Header("Last-Modified", obj.UpdatedAt.Format(time.RFC1123))

	counter := &countingWriter{}

	// Handle multipart objects
	if obj.IsMultipart == consts.ObjectIsMultipartMerged {
		if errno := srv.streamMultipartObject(ctx, obj, c, counter); errno.NotOk() {
			return errno
		}

		if errno := srv.incrementGetObjectMetering(ctx, obj, counter.Count()); errno.NotOk() {
			return errno
		}

		// 触发事件
		go srv.eventService.TriggerEvent(ctx, obj.BucketID, consts.EventTypeGetObject, objectKey, map[string]interface{}{
			"bucket_name": bucketName,
			"object_key":  objectKey,
			"size":        obj.Size,
			"etag":        obj.Etag,
			"transmitted": counter.Count(),
		})

		return common.OK
	}

	// Handle regular objects
	if obj.StoragePath == nil {
		return common.ServerErr.WithMsg("storage path not found")
	}

	file, err := srv.storage.Get(ctx, *obj.StoragePath)
	if err != nil {
		return common.ServerErr.WithErr(err)
	}
	defer file.Close()

	c.Header("Content-Length", fmt.Sprintf("%d", obj.Size))
	if _, err := io.Copy(io.MultiWriter(c.Response.BodyWriter(), counter), file); err != nil {
		return common.ServerErr.WithErr(err)
	}

	transmittedBytes := counter.Count()
	if errno := srv.incrementGetObjectMetering(ctx, obj, transmittedBytes); errno.NotOk() {
		return errno
	}

	// 触发事件
	go srv.eventService.TriggerEvent(ctx, obj.BucketID, consts.EventTypeGetObject, objectKey, map[string]interface{}{
		"bucket_name": bucketName,
		"object_key":  objectKey,
		"size":        obj.Size,
		"etag":        obj.Etag,
		"transmitted": transmittedBytes,
	})

	return common.OK
}

func (srv *Service) incrementGetObjectMetering(ctx *common.UserInfoCtx, obj *do.ObjectDo, transmittedBytes int64) common.Errno {
	bucket, err := srv.bucketRepo.GetByID(ctx, obj.BucketID)
	if err != nil {
		return common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.BucketNotFoundErr)
	}
	if bucket == nil {
		return common.BucketNotFoundErr
	}

	if err := srv.meteringRepo.UpdateDailyMetrics(ctx, bucket.UserID, &bucket.ID, time.Now(), 0, 0, 0, transmittedBytes, 1, 0, 0); err != nil {
		return common.ErrnoFromRepoError(err, common.DatabaseErr)
	}
	return common.OK
}

type countingWriter struct {
	bytes int64
}

func (w *countingWriter) Write(p []byte) (int, error) {
	n := len(p)
	w.bytes += int64(n)
	return n, nil
}

func (w *countingWriter) Count() int64 {
	return w.bytes
}

func (srv *Service) DeleteObject(ctx *common.UserInfoCtx, bucketName, objectKey, versionID string) common.Errno {

	// ── Step 1: 对象级别分布式锁，读之前加，防并发双删 ──────────
	// key 精确到对象，不同对象互不阻塞
	lockKey := fmt.Sprintf("lock:obj:del:%s/%s:%s", bucketName, objectKey, versionID)
	currentWorkdId := uuid.NewString()

	locked, err := srv.locker.AcquireLock(ctx, lockKey, currentWorkdId, 15*time.Second)
	if err != nil {
		return common.ServerErr.WithErr(err)
	}

	if !locked {
		// 拿不到锁说明同一对象正在被另一个请求删除，直接告知客户端
		return common.ConflictErr.WithMsg("object is being deleted, please retry later")
	}
	defer srv.locker.ReleaseLock(ctx, lockKey, currentWorkdId) // 函数退出时释放，无论成功还是失败

	// ── Step 1: 读操作移出事务，各用独立连接 ──────────────────
	obj, err := srv.objRepo.GetByKey(ctx, bucketName, objectKey, versionID)
	if err != nil {
		return common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.ResouceNotFoundErr)
	}

	bucket, err := srv.bucketRepo.GetByUserAndName(ctx, ctx.UserID, bucketName)
	if err != nil {
		return common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.BucketNotFoundErr)
	}

	// ── Step 2: 业务校验在事务外，不占连接 ────────────────────
	if bucket == nil {
		return common.BucketNotFoundErr
	}
	if obj.BucketID != bucket.ID {
		return common.AuthErr
	}

	var versionIDs []string = nil
	var mutilpartUploads []string = nil
	var storagePaths []string = nil
	var totalSize int64 = 0
	var totalNum int64 = 0

	if versionID == "" {

		// 如果删除的是最新版本，找出全部版本
		list, err := srv.objRepo.ListVersionsByFilter(ctx, obj.BucketName, obj.ObjectKey)
		if err != nil {
			return common.ErrnoFromRepoError(err, common.DatabaseErr)
		}
		mutilpartUploads = make([]string, 0, len(list))
		versionIDs = make([]string, 0, len(list))
		storagePaths = make([]string, 0, len(list))
		for _, item := range list {
			versionIDs = append(versionIDs, item.VersionID)
			totalSize += item.Size
			if item.IsMultipart == consts.ObjectIsMultipartMerged {
				mutilpartUploads = append(mutilpartUploads, *item.UploadID)
			} else {
				storagePaths = append(storagePaths, *item.StoragePath)
			}
		}

		totalNum = int64(len(versionIDs))
	}

	if versionIDs == nil {
		versionIDs = []string{versionID}
		totalSize = obj.Size
		totalNum = 1
		mutilpartUploads = []string{}
		storagePaths = []string{}

		if obj.IsMultipart == consts.ObjectIsMultipartMerged && obj.UploadID != nil {
			mutilpartUploads = []string{*obj.UploadID}
		} else {
			storagePaths = []string{*obj.StoragePath}
		}

	}

	err = srv.txManger.RunInTx(ctx, func(ctx1 context.Context, tx tx.Tx) error {

		if err = srv.meteringRepo.WithTx(tx).UpdateDailyMetrics(ctx1, bucket.UserID, &bucket.ID, time.Now(), -totalSize, -totalNum, 0, 0, 0, 0, totalNum); err != nil {
			return err
		}

		if err = srv.objRepo.WithTx(tx).DeleteObject(ctx1, bucketName, objectKey, versionIDs...); err != nil {
			fmt.Printf("err: %v\n", err)
			return err
		}

		if err = srv.userRepo.WithTx(tx).UpdateStorageUsed(ctx1, ctx.UserID, -totalSize); err != nil {
			return err
		}

		if err = srv.bucketRepo.WithTx(tx).UpdateBucketStats(ctx1, ctx.UserID, bucketName, -totalSize, -totalNum); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	for _, uploadId := range mutilpartUploads {
		if err := srv.storage.DeleteParts(ctx, bucketName, uploadId); err != nil {
			srv.logger.Error("failed to delete multipart storage", zap.String("upload_id", uploadId), zap.Error(err))
		}
	}

	for _, storagePath := range storagePaths {
		if err := srv.storage.Delete(ctx, storagePath); err != nil {
			srv.logger.Error("failed to delete storage", zap.String("storage_path", storagePath), zap.Error(err))
		}
	}

	go srv.eventService.TriggerEvent(context.TODO(), obj.BucketID, consts.EventTypeDeleteObject, objectKey, map[string]interface{}{
		"bucket_name": bucketName,
		"object_key":  objectKey,
		"size":        totalSize,
		"etag":        obj.Etag,
	})

	return common.OK
}

// streamMultipartObject 流式返回multipart对象的合并内容
func (srv *Service) streamMultipartObject(ctx *common.UserInfoCtx, obj *do.ObjectDo, c *app.RequestContext, counter io.Writer) common.Errno {
	if obj.UploadID == nil {
		return common.ServerErr.WithMsg("upload_id not found for multipart object")
	}

	// 获取所有分片
	parts, err := srv.multipartRepo.ListMultipartParts(ctx, ctx.UserID, *obj.UploadID)
	if err != nil {
		return common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	if len(parts) == 0 {
		return common.ServerErr.WithMsg("no parts found for multipart object")
	}

	// 按分片号排序
	sort.Slice(parts, func(i, j int) bool {
		return parts[i].PartNumber < parts[j].PartNumber
	})

	// 验证分片连续性
	for i := int32(1); i <= int32(len(parts)); i++ {
		if parts[i-1].PartNumber != i {
			return common.ServerErr.WithMsg("multipart parts are not continuous")
		}
	}

	// 流式输出所有分片
	c.Response.Header.Set("Transfer-Encoding", "chunked")
	c.Response.Header.Del("Content-Length") // 分块传输不能设置Content-Length

	for _, part := range parts {
		if err := func() error {
			file, err := srv.storage.Get(ctx, part.StoragePath)
			if err != nil {
				return err
			}
			defer file.Close()

			_, err = io.Copy(io.MultiWriter(c.Response.BodyWriter(), counter), file)
			return err
		}(); err != nil {
			return common.ServerErr.WithErr(err)
		}
	}

	return common.OK
}
