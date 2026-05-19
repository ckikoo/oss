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
	videoSvc "oss/service/video"
	"oss/utils/ip"
	"oss/utils/logger"
	"oss/utils/tools"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/gogf/gf/util/gconv"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type Service struct {
	txManger       tx.ITxManager
	adaptor        adaptor.IAdaptor
	userRepo       admin.IUser
	objRepo        object.IObjectRepo
	bucketRepo     bucket.IBucketRepo
	multipartRepo  Imultipart.IMultipartRepo
	meteringRepo   metering.IMeteringRepo
	storage        storage.IStorage
	eventService   *event.Service
	locker         redis.ILock
	logger         *zap.Logger
	lifecycleRepo  lifecycle.ILifecycleRepo
	lifeRedis      redis.ILifecycle
	eventRepo      eventI.IEventDeliveryRepo
	eventQueue     redis.IEventQueue
	videoScheduler *videoSvc.Scheduler
	videoCleanup   *videoSvc.CleanupService
}

func NewService(adaptor adaptor.IAdaptor) *Service {
	return &Service{
		txManger:       adaptor.GetTxManager(),
		adaptor:        adaptor,
		userRepo:       gormAdmin.NewUserRepo(adaptor),
		objRepo:        gormObject.NewObjectRepo(adaptor),
		bucketRepo:     gormBucket.NewBucketRepo(adaptor),
		multipartRepo:  gormMultipart.NewObjectRepo(adaptor.GetGORM()),
		meteringRepo:   gormMetering.NewMeteringRepo(adaptor.GetGORM()),
		storage:        adaptor.GetStorage(),
		eventService:   event.NewService(adaptor),
		logger:         logger.GetLogger().With(zap.String("module", "object")),
		lifecycleRepo:  gormLifecycle.NewLifecycleRepo(adaptor.GetGORM()),
		lifeRedis:      redis.NewLifecycle(adaptor),
		locker:         redis.NewLock(adaptor),
		eventRepo:      gormEvent.NewEventDeliveryRepo(adaptor.GetGORM()),
		eventQueue:     redis.NewEventQueue(adaptor),
		videoScheduler: videoSvc.NewScheduler(adaptor),
		videoCleanup:   videoSvc.NewCleanupService(adaptor),
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
	if !objectVisible(obj) {
		return nil, common.ResouceNotFoundErr
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
		IsLatest:     obj.IsLatest,
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
			IsLatest:     obj.IsLatest,
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

	objectKeyHash := tools.Md5Hash(req.ObjectKey)

	oldObject, err := srv.objRepo.GetByKey(ctx, req.BucketName, req.ObjectKey, "")
	if err != nil && !errors.Is(err, repoerr.ErrNotFound) {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}
	if errors.Is(err, repoerr.ErrNotFound) {
		oldObject = nil
	}

	versionID := tools.UUIDHex()
	if bucket.Versioning == consts.BucketVersioningDisabled && objectVisible(oldObject) && !req.Overwrite {
		return nil, common.FileNameExists
	}

	rules, err := srv.lifecycleRepo.ListLifecycleRules(ctx, bucket.ID)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	uInfo, err := srv.userRepo.GetUserInfoById(ctx, ctx.UserID)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	expectedStorageDelta := file.Size
	if bucket.Versioning == consts.BucketVersioningDisabled && objectVisible(oldObject) {
		expectedStorageDelta = file.Size - oldObject.Size
	}
	if uInfo.StorageQuota != 0 && expectedStorageDelta > 0 && uInfo.StorageUsed+expectedStorageDelta > uInfo.StorageQuota {
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
	}

	var id int64
	var oldObjectToCleanup *do.ObjectDo
	deltaCount := int64(0)
	deltaSize := putResult.Size
	if !objectVisible(oldObject) {
		deltaCount = 1
	}
	if bucket.Versioning == consts.BucketVersioningDisabled && objectVisible(oldObject) {
		oldObjectToCleanup = oldObject
		deltaSize = putResult.Size - oldObject.Size
	}

	videoCleanupPlan, err := srv.videoCleanup.PlanObjectVersionCleanup(ctx, oldObjectToCleanup)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}
	if videoCleanupPlan != nil && videoCleanupPlan.DerivedSize > 0 {
		deltaSize -= videoCleanupPlan.DerivedSize
	}

	err = srv.txManger.RunInTx(ctx, func(ctx1 context.Context, tx tx.Tx) error {
		objRepo := srv.objRepo.WithTx(tx)
		if err := objRepo.MarkAllNotLatest(ctx1, createObj.BucketName, createObj.ObjectKey); err != nil {
			return err
		}

		if oldObjectToCleanup != nil {
			if _, err := objRepo.MarkVersionPurged(ctx1, oldObjectToCleanup.BucketName, oldObjectToCleanup.ObjectKey, oldObjectToCleanup.VersionID); err != nil {
				return err
			}
			if oldObjectToCleanup.IsMultipart == consts.ObjectIsMultipartMerged && oldObjectToCleanup.UploadID != nil {
				if err := srv.multipartRepo.WithTx(tx).DeleteMultipartParts(ctx1, ctx.UserID, *oldObjectToCleanup.UploadID); err != nil {
					return err
				}
			}
			if err := srv.videoCleanup.MarkDeletedInTx(ctx1, tx, videoCleanupPlan); err != nil {
				return err
			}
		}

		id, err = objRepo.CreateObject(ctx1, createObj)
		if err != nil {
			return err
		}

		if deltaCount != 0 || deltaSize != 0 {
			if err := srv.bucketRepo.WithTx(tx).UpdateBucketStats(ctx1, ctx.UserID, req.BucketName, deltaCount, deltaSize); err != nil {
				return err
			}
		}

		if deltaSize != 0 {
			if err := srv.userRepo.WithTx(tx).UpdateStorageUsed(ctx1, ctx.UserID, deltaSize); err != nil {
				return err
			}
		}

		return srv.meteringRepo.WithTx(tx).UpdateDailyMetrics(ctx1, bucket.UserID, &bucket.ID, time.Now(), deltaSize, deltaCount, putResult.Size, 0, 0, 1, 0)
	})

	if err != nil {
		if deleteErr := srv.storage.Delete(ctx, putResult.StoragePath); deleteErr != nil {
			srv.logger.Warn("failed to cleanup object storage after PutObject transaction failure",
				zap.String("storage_path", putResult.StoragePath),
				zap.Error(deleteErr))
		}
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	if oldObjectToCleanup != nil {
		srv.deleteObjectStorage(ctx, oldObjectToCleanup)
		srv.videoCleanup.AfterCommit(ctx, videoCleanupPlan)
	}

	srv.scheduleVideoTranscode(ctx, &videoSvc.TranscodeSource{
		UserID:        ctx.UserID,
		BucketID:      bucketID,
		BucketName:    req.BucketName,
		ObjectID:      id,
		ObjectKey:     req.ObjectKey,
		ObjectKeyHash: objectKeyHash,
		VersionID:     versionID,
		SourceEtag:    putResult.Etag,
		SourceSize:    putResult.Size,
		ContentType:   req.ContentType,
	})

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

func (srv *Service) scheduleVideoTranscode(ctx context.Context, source *videoSvc.TranscodeSource) {
	if srv.videoScheduler == nil {
		return
	}
	if err := srv.videoScheduler.ScheduleTranscode(ctx, source); err != nil {
		srv.logger.Warn("failed to schedule video transcode",
			zap.String("bucket_name", source.BucketName),
			zap.String("object_key", source.ObjectKey),
			zap.String("version_id", source.VersionID),
			zap.Error(err))
	}
}

func (srv *Service) GetObject(ctx *common.UserInfoCtx, bucketName, objectKey, versionID string, c *app.RequestContext) common.Errno {
	obj, err := srv.objRepo.GetByKey(ctx, bucketName, objectKey, versionID)
	if err != nil {
		return common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.ResouceNotFoundErr)
	}
	if !objectVisible(obj) {
		return common.ResouceNotFoundErr
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
	bucket, err := srv.bucketRepo.GetByUserAndName(ctx, ctx.UserID, bucketName)
	if err != nil {
		return common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.BucketNotFoundErr)
	}
	if bucket == nil {
		return common.BucketNotFoundErr
	}

	release, errno := srv.acquireObjectWriteLock(ctx, bucketName, objectKey)
	if errno.NotOk() {
		return errno
	}
	defer release()

	obj, err := srv.objRepo.GetByKey(ctx, bucketName, objectKey, versionID)
	if err != nil {
		return common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.ResouceNotFoundErr)
	}
	if obj.BucketID != bucket.ID {
		return common.AuthErr
	}

	if versionID == "" && bucket.Versioning == consts.BucketVersioningEnabled {
		return srv.createDeleteMarker(ctx, bucket, obj)
	}

	if versionID != "" && bucket.Versioning == consts.BucketVersioningDisabled {
		return common.VersioningDisabledErr
	}

	deletedObj, promotedObj, videoCleanupPlan, deltaCount, deltaSize, err := srv.purgeObjectVersion(ctx, bucket, obj)
	if err != nil {
		return common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	srv.deleteObjectStorage(ctx, deletedObj)
	srv.videoCleanup.AfterCommit(ctx, videoCleanupPlan)

	go srv.eventService.TriggerEvent(ctx, obj.BucketID, consts.EventTypeDeleteObject, objectKey, map[string]interface{}{
		"bucket_name": bucketName,
		"object_key":  objectKey,
		"version_id":  obj.VersionID,
		"promoted":    promotedObj != nil,
		"delta_count": deltaCount,
		"size":        -deltaSize,
		"etag":        obj.Etag,
		"purged":      true,
	})

	return common.OK
}

func (srv *Service) RestoreObjectVersion(ctx *common.UserInfoCtx, bucketName, objectKey, versionID string, req *dto.RestoreObjectVersionReq) (*dto.RestoreObjectVersionResp, common.Errno) {
	bucket, err := srv.bucketRepo.GetByUserAndName(ctx, ctx.UserID, bucketName)
	if err != nil {
		return nil, common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.BucketNotFoundErr)
	}
	if bucket == nil {
		return nil, common.BucketNotFoundErr
	}
	if bucket.Versioning == consts.BucketVersioningDisabled {
		return nil, common.VersioningDisabledErr
	}

	release, errno := srv.acquireObjectWriteLock(ctx, bucketName, objectKey)
	if errno.NotOk() {
		return nil, errno
	}
	defer release()

	source, err := srv.objRepo.GetByKey(ctx, bucketName, objectKey, versionID)
	if err != nil {
		return nil, common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.ResouceNotFoundErr)
	}
	if source.BucketID != bucket.ID {
		return nil, common.AuthErr
	}
	if !objectVisible(source) {
		return nil, common.ParamErr.WithMsg("source version is not restorable")
	}

	current, err := srv.objRepo.GetByKey(ctx, bucketName, objectKey, "")
	if err != nil && !errors.Is(err, repoerr.ErrNotFound) {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}
	if errors.Is(err, repoerr.ErrNotFound) {
		current = nil
	}

	uInfo, err := srv.userRepo.GetUserInfoById(ctx, ctx.UserID)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}
	if uInfo.StorageQuota != 0 && source.Size > 0 && uInfo.StorageUsed+source.Size > uInfo.StorageQuota {
		return nil, common.StorageQuotaOver
	}

	newVersionID := tools.UUIDHex()
	putResult, isMultipart, err := srv.copyObjectVersion(ctx, source, newVersionID)
	if err != nil {
		return nil, common.ServerErr.WithErr(err)
	}

	createObj := &do.CreateObject{
		BucketID:      bucket.ID,
		BucketName:    bucketName,
		ObjectKey:     objectKey,
		ObjectKeyHash: source.ObjectKeyHash,
		VersionID:     newVersionID,
		Size:          putResult.Size,
		Etag:          putResult.Etag,
		ContentType:   source.ContentType,
		StorageClass:  source.StorageClass,
		IsMultipart:   isMultipart,
		StoragePath:   &putResult.StoragePath,
		Acl:           source.Acl,
		Metadata:      source.Metadata,
	}

	deltaCount := int64(0)
	if !objectVisible(current) {
		deltaCount = 1
	}

	var objectID int64
	err = srv.txManger.RunInTx(ctx, func(ctx1 context.Context, tx tx.Tx) error {
		objRepo := srv.objRepo.WithTx(tx)
		if err := objRepo.MarkAllNotLatest(ctx1, bucketName, objectKey); err != nil {
			return err
		}
		objectID, err = objRepo.CreateObject(ctx1, createObj)
		if err != nil {
			return err
		}
		if err := srv.bucketRepo.WithTx(tx).UpdateBucketStats(ctx1, ctx.UserID, bucketName, deltaCount, putResult.Size); err != nil {
			return err
		}
		if err := srv.userRepo.WithTx(tx).UpdateStorageUsed(ctx1, ctx.UserID, putResult.Size); err != nil {
			return err
		}
		return srv.meteringRepo.WithTx(tx).UpdateDailyMetrics(ctx1, bucket.UserID, &bucket.ID, time.Now(), putResult.Size, deltaCount, 0, 0, 0, 1, 0)
	})
	if err != nil {
		if deleteErr := srv.storage.Delete(ctx, putResult.StoragePath); deleteErr != nil {
			srv.logger.Warn("failed to cleanup restored object after transaction failure",
				zap.String("storage_path", putResult.StoragePath),
				zap.Error(deleteErr))
		}
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	go srv.eventService.TriggerEvent(ctx, bucket.ID, consts.EventTypePutObject, objectKey, map[string]interface{}{
		"bucket_name":       bucketName,
		"object_key":        objectKey,
		"source_version_id": versionID,
		"version_id":        newVersionID,
		"object_id":         objectID,
		"size":              putResult.Size,
		"etag":              putResult.Etag,
		"restore_reason":    req.Reason,
		"restored":          true,
	})

	return &dto.RestoreObjectVersionResp{
		ObjectKey:       objectKey,
		SourceVersionID: versionID,
		VersionID:       newVersionID,
		Etag:            putResult.Etag,
		Size:            putResult.Size,
	}, common.OK
}

func (srv *Service) createDeleteMarker(ctx *common.UserInfoCtx, bucket *do.BucketDo, latest *do.ObjectDo) common.Errno {
	deltaCount := int64(0)
	if objectVisible(latest) {
		deltaCount = -1
	}

	storageClass := bucket.StorageClass
	if latest.StorageClass != "" {
		storageClass = latest.StorageClass
	}
	if storageClass == "" {
		storageClass = consts.StorageClassStandard
	}
	marker := &do.CreateDeleteMarker{
		BucketID:      bucket.ID,
		BucketName:    bucket.Name,
		ObjectKey:     latest.ObjectKey,
		ObjectKeyHash: latest.ObjectKeyHash,
		VersionID:     tools.UUIDHex(),
		StorageClass:  storageClass,
		Acl:           latest.Acl,
	}

	err := srv.txManger.RunInTx(ctx, func(ctx1 context.Context, tx tx.Tx) error {
		objRepo := srv.objRepo.WithTx(tx)
		if err := objRepo.MarkAllNotLatest(ctx1, marker.BucketName, marker.ObjectKey); err != nil {
			return err
		}
		if _, err := objRepo.CreateDeleteMarker(ctx1, marker); err != nil {
			return err
		}
		if deltaCount != 0 {
			if err := srv.bucketRepo.WithTx(tx).UpdateBucketStats(ctx1, ctx.UserID, bucket.Name, deltaCount, 0); err != nil {
				return err
			}
		}
		return srv.meteringRepo.WithTx(tx).UpdateDailyMetrics(ctx1, bucket.UserID, &bucket.ID, time.Now(), 0, deltaCount, 0, 0, 0, 0, 1)
	})
	if err != nil {
		return common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	srv.videoCleanup.InvalidateObjectVersionTokens(ctx, latest)

	go srv.eventService.TriggerEvent(ctx, bucket.ID, consts.EventTypeDeleteObject, latest.ObjectKey, map[string]interface{}{
		"bucket_name":     bucket.Name,
		"object_key":      latest.ObjectKey,
		"version_id":      marker.VersionID,
		"delete_marker":   true,
		"previous_latest": latest.VersionID,
	})
	return common.OK
}

func (srv *Service) purgeObjectVersion(ctx *common.UserInfoCtx, bucket *do.BucketDo, obj *do.ObjectDo) (*do.ObjectDo, *do.ObjectDo, *videoSvc.ObjectVersionCleanup, int64, int64, error) {
	visibleBefore := objectVisible(obj) && obj.IsLatest == 1
	deltaSize := int64(0)
	if objectVisible(obj) {
		deltaSize = -obj.Size
	}

	videoCleanupPlan, err := srv.videoCleanup.PlanObjectVersionCleanup(ctx, obj)
	if err != nil {
		return nil, nil, nil, 0, 0, err
	}
	if videoCleanupPlan != nil && videoCleanupPlan.DerivedSize > 0 {
		deltaSize -= videoCleanupPlan.DerivedSize
	}

	var deletedObj *do.ObjectDo
	var promotedObj *do.ObjectDo
	err = srv.txManger.RunInTx(ctx, func(ctx1 context.Context, tx tx.Tx) error {
		objRepo := srv.objRepo.WithTx(tx)
		var err error
		deletedObj, err = objRepo.MarkVersionPurged(ctx1, obj.BucketName, obj.ObjectKey, obj.VersionID)
		if err != nil {
			return err
		}
		if err := srv.videoCleanup.MarkDeletedInTx(ctx1, tx, videoCleanupPlan); err != nil {
			return err
		}
		if obj.IsLatest == 1 {
			promotedObj, err = objRepo.PromotePreviousVersion(ctx1, obj.BucketName, obj.ObjectKey)
			if err != nil {
				return err
			}
		}

		visibleAfter := objectVisible(promotedObj)
		deltaCount := boolToInt64(visibleAfter) - boolToInt64(visibleBefore)
		if deltaCount != 0 || deltaSize != 0 {
			if err := srv.bucketRepo.WithTx(tx).UpdateBucketStats(ctx1, ctx.UserID, bucket.Name, deltaCount, deltaSize); err != nil {
				return err
			}
		}
		if deltaSize != 0 {
			if err := srv.userRepo.WithTx(tx).UpdateStorageUsed(ctx1, ctx.UserID, deltaSize); err != nil {
				return err
			}
		}
		if err := srv.meteringRepo.WithTx(tx).UpdateDailyMetrics(ctx1, bucket.UserID, &bucket.ID, time.Now(), deltaSize, deltaCount, 0, 0, 0, 0, 1); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, nil, nil, 0, 0, err
	}

	visibleAfter := objectVisible(promotedObj)
	deltaCount := boolToInt64(visibleAfter) - boolToInt64(visibleBefore)
	return deletedObj, promotedObj, videoCleanupPlan, deltaCount, deltaSize, nil
}

func (srv *Service) acquireObjectWriteLock(ctx context.Context, bucketName, objectKey string) (func(), common.Errno) {
	lockKey := fmt.Sprintf("lock:obj:write:%s/%s", bucketName, objectKey)
	lockID := uuid.NewString()
	locked, err := srv.locker.AcquireLock(ctx, lockKey, lockID, 15*time.Second)
	if err != nil {
		return nil, common.ServerErr.WithErr(err)
	}
	if !locked {
		return nil, common.ConflictErr.WithMsg("object is being modified, please retry later")
	}

	return func() {
		if err := srv.locker.ReleaseLock(ctx, lockKey, lockID); err != nil {
			srv.logger.Warn("failed to release object write lock",
				zap.String("lock_key", lockKey),
				zap.Error(err))
		}
	}, common.OK
}

func (srv *Service) deleteObjectStorage(ctx context.Context, obj *do.ObjectDo) {
	if obj == nil || obj.Status == consts.ObjectStatusDeleteMark {
		return
	}
	switch obj.IsMultipart {
	case consts.ObjectIsMultipartMerged:
		if obj.UploadID == nil {
			return
		}
		if err := srv.storage.DeleteParts(ctx, obj.BucketName, *obj.UploadID); err != nil {
			srv.logger.Error("failed to delete multipart storage", zap.String("upload_id", *obj.UploadID), zap.Error(err))
		}
	default:
		if obj.StoragePath == nil {
			return
		}
		if err := srv.storage.Delete(ctx, *obj.StoragePath); err != nil {
			srv.logger.Error("failed to delete object storage", zap.String("storage_path", *obj.StoragePath), zap.Error(err))
		}
	}
}

func (srv *Service) copyObjectVersion(ctx *common.UserInfoCtx, source *do.ObjectDo, newVersionID string) (*storage.PutResult, int32, error) {
	if source.IsMultipart == consts.ObjectIsMultipartMerged {
		if source.UploadID == nil {
			return nil, 0, fmt.Errorf("multipart object missing upload_id")
		}
		parts, err := srv.multipartRepo.ListMultipartParts(ctx, ctx.UserID, *source.UploadID)
		if err != nil {
			return nil, 0, err
		}
		if len(parts) == 0 {
			return nil, 0, fmt.Errorf("multipart object has no parts")
		}
		sort.Slice(parts, func(i, j int) bool {
			return parts[i].PartNumber < parts[j].PartNumber
		})
		partPaths := make([]string, 0, len(parts))
		for _, part := range parts {
			partPaths = append(partPaths, part.StoragePath)
		}
		result, err := srv.storage.MergeParts(ctx, source.BucketName, source.ObjectKey, newVersionID, partPaths)
		return result, consts.ObjectIsMultipartNormal, err
	}

	if source.StoragePath == nil {
		return nil, 0, fmt.Errorf("object storage path not found")
	}
	file, err := srv.storage.Get(ctx, *source.StoragePath)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()

	result, err := srv.storage.Put(ctx, source.BucketName, source.ObjectKey, newVersionID, file)
	return result, consts.ObjectIsMultipartNormal, err
}

func objectVisible(obj *do.ObjectDo) bool {
	return obj != nil && obj.Status == consts.ObjectStatusNormal
}

func boolToInt64(v bool) int64 {
	if v {
		return 1
	}
	return 0
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
