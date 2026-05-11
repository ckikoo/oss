package object

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"sort"
	"time"

	"oss/adaptor"
	"oss/adaptor/redis"
	"oss/adaptor/repo/admin"
	gormAdmin "oss/adaptor/repo/admin/gorm"
	"oss/adaptor/repo/bucket"
	gormBucket "oss/adaptor/repo/bucket/gorm"
	"oss/adaptor/repo/metering"
	gormMetering "oss/adaptor/repo/metering/gorm"
	Imultipart "oss/adaptor/repo/multipart"
	gormMultipart "oss/adaptor/repo/multipart/gorm"
	"oss/adaptor/repo/object"
	gormObject "oss/adaptor/repo/object/gorm"
	"oss/adaptor/storage"
	"oss/adaptor/tx"
	"oss/common"
	"oss/consts"
	"oss/service/do"
	"oss/service/dto"
	"oss/service/event"
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
}

func NewService(adaptor adaptor.IAdaptor) *Service {
	return &Service{
		txManger:      adaptor.GetTxManager(),
		adaptor:       adaptor,
		userRepo:      gormAdmin.NewUserRepo(adaptor.GetGORM()),
		objRepo:       gormObject.NewObjectRepo(adaptor.GetGORM()),
		bucketRepo:    gormBucket.NewBucketRepo(adaptor.GetGORM()),
		multipartRepo: gormMultipart.NewObjectRepo(adaptor.GetGORM()),
		meteringRepo:  gormMetering.NewMeteringRepo(adaptor.GetGORM()),
		storage:       adaptor.GetStorage(),
		eventService:  event.NewService(adaptor),
	}
}

func (srv *Service) ListObjects(ctx *common.UserInfoCtx, req *dto.ListObjectsReq) (*dto.ListObjectsResp, common.Errno) {
	if req.BucketName == "" {
		return nil, common.ParamErr.WithMsg("bucket_name is required")
	}

	objects, err := srv.objRepo.ListByFilter(ctx, req.BucketName, req.Prefix, req.Delimiter, req.Marker, req.MaxKeys, req.VersionID)
	if err != nil {
		return nil, common.DatabaseErr.WithErr(err)
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
		return nil, common.DatabaseErr.WithErr(err)
	}

	if bucket == nil {
		return nil, common.BucketNotFoundErr
	}

	obj, err := srv.objRepo.GetByKey(ctx, bucketName, objectKey, versionID)
	if err != nil {
		return nil, common.ResouceNotFoundErr.WithErr(err)
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

func (srv *Service) PutObject(ctx *common.UserInfoCtx, req *dto.PutObjectReq, file *multipart.FileHeader) (*dto.PutObjectResp, common.Errno) {

	bucket, err := srv.bucketRepo.GetByName(ctx, ctx.UserID, req.BucketName)
	if err != nil {
		return nil, common.ParamErr.WithMsg("bucket not found")
	}
	bucketID := bucket.ID

	if err := srv.meteringRepo.UpdateDailyMetrics(ctx, bucket.UserID, &bucket.ID, time.Now(), 0, 0, file.Size, 0, 1, 0, 0); err != nil {
		logger.Error("object PutObject meteringRepo.UpdateDailyMetrics error", zap.Error(err), zap.String("req", gconv.String(req)), zap.Int64("uploadSize", (file.Size)))
	}

	// Generate object key hash
	// 需要优化
	objectKeyHash := tools.Md5Hash(req.ObjectKey)

	// Check if object already exists
	cacheFile, err := srv.objRepo.GetObjectFromHashKey(ctx, &do.GetObjectFromHashKey{
		BucketName:    req.BucketName,
		ObjectKeyHash: objectKeyHash,
	})
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, common.DatabaseErr.WithErr(err)
	}

	// TODO 应该支持覆盖才对
	if cacheFile != nil {
		return nil, common.FileNameExists
	}

	uInfo, err := srv.userRepo.GetUserInfoById(ctx, ctx.UserID)
	if err != nil {
		return nil, common.DatabaseErr.WithErr(err)
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

	putResult, err := srv.storage.Put(ctx, req.BucketName, req.ObjectKey, f)
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
		VersionID:     "", // TODO: Handle versioning
		Size:          putResult.Size,
		Etag:          putResult.Etag,
		ContentType:   &req.ContentType,
		StorageClass:  storageClass,
		IsMultipart:   consts.ObjectIsMultipartNormal,

		StoragePath: &putResult.StoragePath,
		Acl:         req.Acl,
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
			return srv.userRepo.WithTx(tx).UpdateStorageUsed(ctx, ctx.UserID, putResult.Size)
		},
	}

	_, err = srv.objRepo.CreateObject(ctx, createObj)
	if err != nil {
		srv.storage.Delete(ctx, putResult.StoragePath)
		return nil, common.DatabaseErr.WithErr(err)
	}

	// 触发事件
	go srv.eventService.TriggerEvent(ctx, bucketID, consts.EventTypePutObject, req.ObjectKey, map[string]interface{}{
		"bucket_name":   req.BucketName,
		"object_key":    req.ObjectKey,
		"size":          putResult.Size,
		"etag":          putResult.Etag,
		"content_type":  req.ContentType,
		"storage_class": storageClass,
	})

	return &dto.PutObjectResp{
		ObjectKey:   req.ObjectKey,
		Size:        putResult.Size,
		Etag:        putResult.Etag,
		StoragePath: putResult.StoragePath,
		VersionID:   "",
	}, common.OK
}

func (srv *Service) GetObject(ctx *common.UserInfoCtx, bucketName, objectKey, versionID string, c *app.RequestContext) common.Errno {
	obj, err := srv.objRepo.GetByKey(ctx, bucketName, objectKey, versionID)
	if err != nil {
		return common.DatabaseErr.WithErr(err)
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
		return common.DatabaseErr.WithErr(err)
	}

	if err := srv.meteringRepo.UpdateDailyMetrics(ctx, bucket.UserID, &bucket.ID, time.Now(), 0, 0, 0, transmittedBytes, 1, 0, 0); err != nil {
		return common.DatabaseErr.WithErr(err)
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
		return common.DatabaseErr.WithErr(err)
	}

	bucket, err := srv.bucketRepo.GetByUserAndName(ctx, ctx.UserID, bucketName)
	if err != nil {
		return common.DatabaseErr.WithErr(err)
	}

	// ── Step 2: 业务校验在事务外，不占连接 ────────────────────
	if obj.BucketID != bucket.ID {
		return common.AuthErr
	}

	err = srv.txManger.RunInTx(ctx, func(tx tx.Tx) error {

		if err := srv.meteringRepo.WithTx(tx).UpdateDailyMetrics(ctx, bucket.UserID, &bucket.ID, time.Now(), -obj.Size, -1, 0, 0, 0, 0, 1); err != nil {
			return err
		}

		if err := srv.objRepo.WithTx(tx).DeleteObject(ctx, bucketName, objectKey, versionID); err != nil {
			return err
		}

		if err := srv.userRepo.WithTx(tx).UpdateStorageUsed(ctx, ctx.UserID, -obj.Size); err != nil {
			return err
		}

		if obj.IsMultipart == consts.ObjectIsMultipartMerged && obj.UploadID != nil {
			if err := srv.multipartRepo.WithTx(tx).DeleteMultipartParts(ctx, ctx.UserID, *obj.UploadID); err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return common.DatabaseErr.WithErr(err)
	}

	// 对文件进行删除
	// TODO 需要引入一个sanbox 异步执行删除，避免删除文件的慢操作影响用户体验，目前mvp 先同步删除，后续优化
	if obj != nil && obj.IsMultipart == consts.ObjectIsMultipartMerged && obj.UploadID != nil {
		if err := srv.storage.DeleteParts(ctx, obj.BucketName, *obj.UploadID); err != nil {
			logger.GetLogger().Error("failed to delete multipart storage parts", zap.String("upload_id", *obj.UploadID), zap.Error(err))
		}
	} else if obj != nil && obj.StoragePath != nil {
		if err := srv.storage.Delete(ctx, *obj.StoragePath); err != nil {
			logger.GetLogger().Error("failed to delete object storage file", zap.String("storage_path", *obj.StoragePath), zap.Error(err))
		}
	}

	go srv.eventService.TriggerEvent(context.TODO(), obj.BucketID, consts.EventTypeDeleteObject, objectKey, map[string]interface{}{
		"bucket_name": bucketName,
		"object_key":  objectKey,
		"size":        obj.Size,
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
		return common.DatabaseErr.WithErr(err)
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
