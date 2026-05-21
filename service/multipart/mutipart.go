package multipart

import (
	"context"
	"errors"
	"fmt"
	"io"
	"oss/adaptor"
	"oss/adaptor/redis"
	"oss/adaptor/repo/admin"
	gormAdmin "oss/adaptor/repo/admin/gorm"
	"oss/adaptor/repo/async"
	gormAsync "oss/adaptor/repo/async/gorm"
	"oss/adaptor/repo/bucket"
	gormBucket "oss/adaptor/repo/bucket/gorm"
	eventI "oss/adaptor/repo/event"
	gormEvent "oss/adaptor/repo/event/gorm"
	"oss/adaptor/repo/metering"
	gormMetering "oss/adaptor/repo/metering/gorm"
	"oss/adaptor/repo/multipart"
	gormMultipart "oss/adaptor/repo/multipart/gorm"
	"oss/adaptor/repo/object"
	gormObject "oss/adaptor/repo/object/gorm"
	"oss/adaptor/repo/repoerr"
	"oss/adaptor/storage"
	"oss/adaptor/tx"
	"oss/common"
	"oss/config"
	"oss/consts"
	"oss/service/do"
	"oss/service/dto"
	"oss/service/event"
	videoSvc "oss/service/video"
	"oss/utils/ip"
	"oss/utils/logger"
	"oss/utils/tools"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gogf/gf/util/gconv"
	"go.uber.org/zap"
)

type Service struct {
	txManger      tx.ITxManager
	userRepo      admin.IUser
	objRepo       object.IObjectRepo
	multipartRepo multipart.IMultipartRepo
	bucketRepo    bucket.IBucketRepo
	rdsmultipart  redis.IMultipart
	storage       storage.IStorage
	asyncRepo     async.IAsyncTaskRepo
	asyncRedis    redis.ITask
	eventService  *event.Service
	tokenRedis    redis.IToken
	meteringRepo  metering.IMeteringRepo
	logger        *zap.Logger
	eventRepo     eventI.IEventDeliveryRepo
	eventQueue    redis.IEventQueue
	videoCleanup  *videoSvc.CleanupService
}

func NewService(adaptor adaptor.IAdaptor) *Service {
	return &Service{
		txManger:      adaptor.GetTxManager(),
		userRepo:      gormAdmin.NewUserRepo(adaptor),
		objRepo:       gormObject.NewObjectRepo(adaptor),
		bucketRepo:    gormBucket.NewBucketRepo(adaptor),
		multipartRepo: gormMultipart.NewObjectRepo(adaptor.GetGORM()),
		rdsmultipart:  redis.NewMultipart(adaptor),
		storage:       adaptor.GetStorage(),
		asyncRepo:     gormAsync.NewAsyncTaskRepo(adaptor.GetGORM()),
		asyncRedis:    redis.NewTask(adaptor),
		eventService:  event.NewService(adaptor),
		tokenRedis:    redis.NewToken(adaptor),
		meteringRepo:  gormMetering.NewMeteringRepo(adaptor.GetGORM()),
		eventRepo:     gormEvent.NewEventDeliveryRepo(adaptor.GetGORM()),
		eventQueue:    redis.NewEventQueue(adaptor),
		videoCleanup:  videoSvc.NewCleanupService(adaptor),
		logger:        logger.GetLogger().With(zap.String("module", "multipart")),
	}
}
func (srv *Service) CreateMultipartUpload(ctx *common.UserInfoCtx, bucketName string, req *dto.CreateMultipartUploadReq) (*dto.CreateMultipartUploadResp, common.Errno) {

	if req.CallbackUrl != "" && config.GlobalConfig.Server.Env != "dev" {
		err := ip.ValidateCallbackURL(req.CallbackUrl)
		if err != nil {
			return nil, common.ParamErr.WithErr(err)
		}
	}

	if req.ObjectKey == "" {
		return nil, common.ParamErr.WithMsg("object_key is required")
	}
	if req.TotalChunk <= 0 {
		return nil, common.ParamErr.WithMsg("total_chunk must greate zero")
	}

	bucket, err := srv.bucketRepo.GetByName(ctx, ctx.UserID, bucketName)
	if err != nil {
		return nil, common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.BucketNotFoundErr)
	}

	temp, err := srv.objRepo.GetByKey(ctx, bucketName, req.ObjectKey, "")
	if err != nil && !errors.Is(err, repoerr.ErrNotFound) {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	switch bucket.Versioning {
	case consts.BucketVersioningDisabled:
		if temp != nil && !req.Overwrite {
			return nil, common.FileNameExists
		}
	default: // suspended
		if temp != nil && !req.Overwrite {
			return nil, common.FileNameExists
		}
	}

	uInfo, err := srv.userRepo.GetUserInfoById(ctx, ctx.UserID)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	if uInfo.StorageQuota > 0 && uInfo.StorageUsed+req.FileSize > uInfo.StorageQuota {
		return nil, common.StorageQuotaOver
	}

	uploadID := tools.UUIDHex()
	objectKeyHash := tools.Md5Hash(req.ObjectKey)
	storageClass := req.StorageClass
	if storageClass == "" {
		storageClass = consts.StorageClassStandard
	}

	createUpload := &do.CreateMultipartUpload{
		UploadID:      uploadID,
		BucketID:      bucket.ID,
		BucketName:    bucketName,
		ObjectKey:     req.ObjectKey,
		ObjectKeyHash: objectKeyHash,
		UserID:        bucket.UserID,
		TotalChunk:    req.TotalChunk,
		VersionID:     tools.UUIDHex(),
		UploadedChunk: 0,
		Status:        consts.MultipartUploadStatusUploading,
		StorageClass:  &storageClass,
		ContentType:   &req.ContentType,
		Metadata: func() *string {
			if req.Metadata != "" {
				return &req.Metadata
			}

			return nil
		}(),
		ExpiresAt:    time.Now().Add(24 * time.Hour),
		LastActiveAt: time.Now(),
	}
	defer func() {
		if err != nil {
			srv.rdsmultipart.DelTimeoutMultipartCancel(ctx, uploadID)
		}
	}()

	if _, err = srv.multipartRepo.CreateMultipartUpload(ctx, createUpload); err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	srv.tokenRedis.CreateUploadToken(ctx, uploadID, &dto.CreateUploadTokenReq{
		UserId:      ctx.UserID,
		BucketName:  bucketName,
		ObjectKey:   req.ObjectKey,
		ExpiresIn:   createUpload.ExpiresAt.Unix(),
		SizeLimit:   req.SizeLimit,
		Overwrite:   req.Overwrite,
		CallbackUrl: req.CallbackUrl,
	}, time.Hour*24)

	err = srv.rdsmultipart.SetTimeoutMultipartCancel(ctx, uploadID, createUpload.ExpiresAt)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	return &dto.CreateMultipartUploadResp{
		UploadID:   uploadID,
		BucketID:   bucket.ID,
		ObjectKey:  req.ObjectKey,
		TotalChunk: req.TotalChunk,
		Status:     consts.MultipartUploadStatusUploading,
		ExpiresAt:  createUpload.ExpiresAt.UnixMilli(),
	}, common.OK
}

// upload_etag 参数用于校验文件是否和用户传递的一致，防止用户上传了错误的文件
// uploadID参数用于校验用户是否有权限上传这个文件
func (srv *Service) UploadMultipartPart(
	ctx *common.UserInfoCtx,
	token string,
	upload_etag string,
	uploadID string,
	partNumber int32,
	reader io.Reader,
	totolSize int64,
) (*dto.UploadMultipartPartResp, common.Errno) {

	limitMap, err := srv.tokenRedis.GetUploadTokenFields(
		ctx,
		token,
		redis.FieldSizeLimit,
		redis.FieldBucketName,
		redis.FieldObjectKey,
		redis.FieldExpiresIn,
	)
	if err != nil {
		return nil, common.RedisErr.WithErr(err)
	}

	bucketName := limitMap[redis.FieldBucketName]

	bucket, err := srv.bucketRepo.GetByName(ctx, ctx.UserID, bucketName)
	if err != nil {
		srv.logger.Error(
			"UploadMultipartPart bucketRepo.GetByName error",
			zap.Error(err),
			zap.String("redis json", gconv.String(limitMap)),
		)
	}

	if bucket != nil {
		if err := srv.meteringRepo.UpdateDailyMetrics(
			ctx,
			bucket.UserID,
			&bucket.ID,
			time.Now(),
			0,
			0,
			totolSize,
			0,
			1,
			0,
			0,
		); err != nil {
			srv.logger.Error(
				"UploadMultipartPart meteringRepo.UpdateDailyMetrics error",
				zap.Error(err),
				zap.String("redis json", gconv.String(limitMap)),
			)
		}
	}

	newPartSize := totolSize

	if limitMap[redis.FieldSizeLimit] != "" && limitMap[redis.FieldSizeLimit] != "0" {
		limit, err := strconv.ParseInt(limitMap[redis.FieldSizeLimit], 10, 64)
		if err != nil {
			return nil, common.ErrInternalServer.WithErr(err)
		}

		if newPartSize > limit {
			return nil, common.FilePartSizeOutLimit
		}
	}

	upload, err := srv.multipartRepo.GetMultipartUploadByID(ctx, ctx.UserID, uploadID)
	if err != nil {
		if errors.Is(err, repoerr.ErrNotFound) {
			return nil, common.FileUploadIdNotFound
		}
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	// 只有上传中状态才允许上传分片，已完成、已中止的上传不允许再上传分片
	if upload.Status != consts.MultipartUploadStatusUploading {
		return nil, common.FileUploadIdStatusNotOnUpload
	}

	if partNumber <= 0 || partNumber > upload.TotalChunk {
		return nil, common.ParamErr.WithMsg("part_number exceeds total_chunk")
	}

	uinfo, err := srv.userRepo.GetUserInfoById(ctx, ctx.UserID)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	// 重点：查旧 part。
	// 重复上传同一个 partNumber 时，只按新旧 size 差值更新容量。
	var oldPart *do.MultipartPartDo
	oldPartSize := int64(0)

	oldPart, err = srv.multipartRepo.GetMultipartPart(ctx, ctx.UserID, uploadID, partNumber)
	if err != nil {
		if !errors.Is(err, repoerr.ErrNotFound) {
			return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
		}
	} else if oldPart != nil {
		oldPartSize = oldPart.Size
	}

	deltaSize := newPartSize - oldPartSize

	// 只有 deltaSize > 0 时才会新增占用容量。
	// deltaSize <= 0 表示重复上传同大小 part，或者替换成更小 part。
	if uinfo.StorageQuota > 0 &&
		deltaSize > 0 &&
		uinfo.StorageUsed+deltaSize > uinfo.StorageQuota {
		return nil, common.StorageQuotaOver
	}

	res, err := srv.storage.PutPart(
		ctx,
		upload.BucketName,
		uploadID,
		partNumber,
		reader,
	)
	if err != nil {
		return nil, common.ServerErr.WithErr(err)
	}

	// 客户端传了 Content-MD5 / ETag 才校验。
	// 不传就跳过，否则 upload_etag 为空会导致所有分片上传失败。
	if upload_etag != "" && res.Etag != upload_etag {
		_ = srv.storage.DeletePart(ctx, upload.BucketName, uploadID, partNumber)
		return nil, common.FileCheckErr
	}

	dataSize := res.Size
	if dataSize <= 0 {
		dataSize = newPartSize
	}

	// storage 返回的真实大小可能和 len(data) 不一致，所以这里重新算一次 delta。
	deltaSize = dataSize - oldPartSize

	part := &do.CreateMultipartPart{
		UploadID:    uploadID,
		PartNumber:  partNumber,
		Size:        dataSize,
		Etag:        res.Etag,
		StoragePath: res.StoragePath,
		Status:      consts.MultipartPartStatusConfirmed,
	}

	err = srv.txManger.RunInTx(ctx, func(ctx1 context.Context, tx tx.Tx) error {
		multipartRepo := srv.multipartRepo.WithTx(tx)
		userRepo := srv.userRepo.WithTx(tx)
		bucketRepo := srv.bucketRepo.WithTx(tx)

		if _, err := multipartRepo.CreateOrUpdateMultipartPart(ctx1, part); err != nil {
			return err
		}

		lastActive := time.Now()
		update := &do.UpdateMultipartUpload{
			LastActiveAt: &lastActive,
		}

		if _, err := multipartRepo.UpdateMultipartUpload(ctx1, ctx.UserID, uploadID, update); err != nil {
			return err
		}

		// 重点：只按差值更新容量。
		//
		// 第一次上传 part：
		// oldPartSize = 0，dataSize = 5MB，deltaSize = +5MB
		//
		// 重复上传同大小 part：
		// oldPartSize = 5MB，dataSize = 5MB，deltaSize = 0
		//
		// 替换成更大的 part：
		// oldPartSize = 5MB，dataSize = 8MB，deltaSize = +3MB
		//
		// 替换成更小的 part：
		// oldPartSize = 8MB，dataSize = 5MB，deltaSize = -3MB
		if deltaSize != 0 {
			if err := userRepo.UpdateStorageUsed(ctx1, ctx.UserID, deltaSize); err != nil {
				return err
			}

			if err := bucketRepo.UpdateBucketStats(ctx1, ctx.UserID, bucketName, 0, deltaSize); err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		// 第一次上传时，事务失败可以删掉刚写入的 part。
		// 但是重复上传旧 part 时，不能无脑 DeletePart，
		// 否则可能把旧 part 的物理文件也删掉，导致 DB 还指向旧 part，但存储没了。
		if oldPart == nil {
			_ = srv.storage.DeletePart(ctx, upload.BucketName, uploadID, partNumber)
		}

		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	return &dto.UploadMultipartPartResp{
		PartNumber: partNumber,
		Etag:       res.Etag,
		Size:       dataSize,
		Status:     consts.MultipartPartStatusConfirmed,
	}, common.OK
}

// 先做的是伪合并逻辑， 任务放入redis ，由后台工作线程异步执行真正的合并，合并完成后更新状态为已合并
func (srv *Service) CompleteMultipartUpload(ctx *common.UserInfoCtx, uploadID string, bucketName string, req *dto.CompleteMultipartUploadReq) (*dto.CompleteMultipartUploadResp, common.Errno) {
	if len(req.Parts) == 0 {
		return nil, common.ParamErr.WithMsg("parts are required")
	}

	upload, err := srv.multipartRepo.GetMultipartUploadByID(ctx, ctx.UserID, uploadID)
	if err != nil {
		return nil, common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.FileUploadIdNotFound)
	}

	if upload.Status != consts.MultipartUploadStatusUploading {
		return nil, common.FileUploadIdStatusNotOnUpload
	}
	if int32(len(req.Parts)) != upload.TotalChunk {
		return nil, common.ParamErr.WithMsg("parts count not match total_chunk")
	}
	bucket, err := srv.bucketRepo.GetByName(ctx, ctx.UserID, bucketName)
	if err != nil {
		return nil, common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.BucketNotFoundErr)
	}
	if upload.BucketID != bucket.ID || upload.BucketName != bucketName {
		return nil, common.FileUploadIdNotFound.WithMsg("upload_id does not belong to bucket")
	}

	oldObject, err := srv.objRepo.GetByKey(ctx, bucketName, upload.ObjectKey, "")
	if err != nil && !errors.Is(err, repoerr.ErrNotFound) {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}
	if errors.Is(err, repoerr.ErrNotFound) {
		oldObject = nil
	}

	createReqMap, err := srv.tokenRedis.GetUploadTokenFields(ctx, uploadID, redis.FieldCallbackURL)
	if err != nil {
		return nil, common.RedisErr.WithErr(err)
	}

	storedParts, err := srv.multipartRepo.ListMultipartParts(ctx, ctx.UserID, uploadID)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	if len(storedParts) == 0 {
		return nil, common.ParamErr.WithMsg("no multipart parts uploaded")
	}

	partIndex := map[int32]*do.MultipartPartDo{}
	totalSize := int64(0)
	for _, part := range storedParts {
		partIndex[part.PartNumber] = part
		totalSize += part.Size
	}

	for _, part := range req.Parts {
		stored, ok := partIndex[part.PartNumber]
		if !ok {
			return nil, common.FilePartNotFound.WithMsg(fmt.Sprintf("missing part %d", part.PartNumber))
		}
		if stored.Etag != part.Etag {
			return nil, common.ParamErr.WithMsg(fmt.Sprintf("etag mismatch for part %d", part.PartNumber))
		}
	}

	sortedParts := make([]*do.MultipartPartDo, 0, len(req.Parts))
	for _, part := range req.Parts {
		sortedParts = append(sortedParts, partIndex[part.PartNumber])
	}

	sort.Slice(sortedParts, func(i, j int) bool {
		return sortedParts[i].PartNumber < sortedParts[j].PartNumber
	})

	for i := int32(1); i <= int32(len(sortedParts)); i++ {
		if sortedParts[i-1].PartNumber != i {
			return nil, common.ParamErr.WithMsg("multipart parts must be numbered from 1 to n without gaps")
		}
	}

	sb := strings.Builder{}

	for _, part := range sortedParts {
		sb.WriteString(part.Etag)
	}

	sb.WriteString(fmt.Sprintf("-%v", len(sortedParts)))

	resultEtag := tools.Md5Hash(sb.String())

	storageClass := consts.StorageClassStandard
	if upload.StorageClass != nil && *upload.StorageClass != "" {
		storageClass = *upload.StorageClass
	}
	contentType := ""
	if upload.ContentType != nil {
		contentType = *upload.ContentType
	}
	var metadata *string
	if upload.Metadata != nil && *upload.Metadata != "" {
		metadata = upload.Metadata
	}

	var statusMerged int32 = consts.MultipartUploadStatusMergedVirtual
	lastActive := time.Now()
	update := &do.UpdateMultipartUpload{
		Status:       &statusMerged,
		LastActiveAt: &lastActive,
	}
	if upload.TotalChunk > 0 {
		totalChunk := upload.TotalChunk
		update.TotalChunk = &totalChunk
	}

	createObj := &do.CreateObject{
		BucketID:      upload.BucketID,
		BucketName:    upload.BucketName,
		ObjectKey:     upload.ObjectKey,
		ObjectKeyHash: upload.ObjectKeyHash,
		VersionID:     upload.VersionID,
		Size:          totalSize,
		Etag:          resultEtag,
		ContentType:   &contentType,
		StorageClass:  storageClass,
		IsMultipart:   consts.ObjectIsMultipartMerged,
		UploadID:      &uploadID,
		Acl:           consts.ObjectAclInheritBucket,
		Metadata:      metadata,
	}

	var objectID int64
	var oldObjectToCleanup *do.ObjectDo
	deltaCount := int64(0)
	deltaSize := int64(0)
	if oldObject == nil || oldObject.Status != consts.ObjectStatusNormal {
		deltaCount = 1
	}
	if bucket.Versioning == consts.BucketVersioningDisabled && oldObject != nil && oldObject.Status == consts.ObjectStatusNormal {
		oldObjectToCleanup = oldObject
		deltaSize = -oldObject.Size
	}

	videoCleanupPlan, err := srv.videoCleanup.PlanObjectVersionCleanup(ctx, oldObjectToCleanup)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}
	if videoCleanupPlan != nil && videoCleanupPlan.DerivedSize > 0 {
		deltaSize -= videoCleanupPlan.DerivedSize
	}

	var taskID int64
	err = srv.txManger.RunInTx(ctx, func(ctx1 context.Context, tx tx.Tx) error {
		objRepo := srv.objRepo.WithTx(tx)
		if err := objRepo.MarkAllNotLatest(ctx1, bucketName, upload.ObjectKey); err != nil {
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

		objectID, err = objRepo.CreateObject(ctx1, createObj)
		if err != nil {
			return err
		}

		if _, err := srv.multipartRepo.WithTx(tx).UpdateMultipartUpload(ctx1, ctx.UserID, uploadID, update); err != nil {
			return err
		}

		createdTaskID, err := srv.asyncRepo.WithTx(tx).CreateAsyncTask(ctx1, &do.CreateAsyncTask{
			UserId:   ctx.UserID,
			TaskType: consts.TaskTypePhysicalMerge,
			BizType:  consts.TaskBizTypeUpload,
			BizID:    uploadID,
			Status:   consts.TaskStatusPending,
			MaxRetry: 3,
		})
		if err != nil {
			return err
		}
		taskID = createdTaskID

		if deltaCount != 0 || deltaSize != 0 {
			if err := srv.bucketRepo.WithTx(tx).UpdateBucketStats(ctx1, ctx.UserID, bucketName, deltaCount, deltaSize); err != nil {
				return err
			}
		}

		if deltaSize != 0 {
			if err := srv.userRepo.WithTx(tx).UpdateStorageUsed(ctx1, ctx.UserID, deltaSize); err != nil {
				return err
			}
		}

		return srv.meteringRepo.WithTx(tx).UpdateDailyMetrics(ctx1, bucket.UserID, &bucket.ID, time.Now(), deltaSize, deltaCount, 0, 0, 0, 1, 0)
	})

	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	if oldObjectToCleanup != nil {
		switch oldObjectToCleanup.IsMultipart {
		case consts.ObjectIsMultipartMerged:
			if oldObjectToCleanup.UploadID != nil {
				if err := srv.storage.DeleteParts(ctx, oldObjectToCleanup.BucketName, *oldObjectToCleanup.UploadID); err != nil {
					srv.logger.Warn("failed to delete old multipart parts storage",
						zap.String("bucket_name", oldObjectToCleanup.BucketName),
						zap.String("upload_id", *oldObjectToCleanup.UploadID),
						zap.Error(err))
				}
			}
		default:
			if oldObjectToCleanup.StoragePath != nil {
				if err := srv.storage.Delete(ctx, *oldObjectToCleanup.StoragePath); err != nil {
					srv.logger.Warn("failed to delete old object storage",
						zap.String("storage_path", *oldObjectToCleanup.StoragePath),
						zap.Error(err))
				}
			}
		}
		srv.videoCleanup.AfterCommit(ctx, videoCleanupPlan)
	}

	srv.rdsmultipart.DelTimeoutMultipartCancel(ctx, uploadID)

	if err := srv.enqueueAsyncTask(ctx, taskID); err != nil {
		srv.logger.Warn("failed to enqueue physical merge task, pending scanner will retry",
			zap.Int64("task_id", taskID),
			zap.String("upload_id", uploadID),
			zap.Int64("object_id", objectID),
			zap.Error(err))
	}

	// 回调事件
	if createReqMap[redis.FieldCallbackURL] != "" {
		callbackURL := createReqMap[redis.FieldCallbackURL]
		callbackPayload := map[string]interface{}{
			"callback_url": callbackURL,
			"event_type":   "multipart_complete",
			"bucket_name":  upload.BucketName,
			"object_key":   upload.ObjectKey,
			"upload_id":    uploadID,
			"object_id":    objectID,
			"version_id":   upload.VersionID,
			"size":         totalSize,
			"etag":         resultEtag,
			"parts_count":  len(sortedParts),
			"status":       "completed",
		}
		srv.dispatchCallback(ctx, callbackURL, callbackPayload)
	}

	// 触发事件
	go srv.eventService.TriggerEvent(ctx, upload.BucketID, consts.EventTypeMultipartComplete, upload.ObjectKey, map[string]interface{}{
		"bucket_name": upload.BucketName,
		"object_key":  upload.ObjectKey,
		"upload_id":   uploadID,
		"version_id":  upload.VersionID,
		"size":        totalSize,
		"etag":        resultEtag,
		"parts_count": len(sortedParts),
	})

	return &dto.CompleteMultipartUploadResp{
		ObjectID:  objectID,
		ObjectKey: upload.ObjectKey,
		VersionID: upload.VersionID,
		Status:    statusMerged,
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

func (srv *Service) publishTask(ctx *common.UserInfoCtx, taskType string, uploadID string) error {
	if !consts.ValidAsyncTaskType(taskType) {
		return fmt.Errorf("invalid async task type: %s", taskType)
	}

	task := &do.CreateAsyncTask{
		UserId:   ctx.UserID,
		TaskType: taskType,
		BizType:  consts.TaskBizTypeUpload,
		BizID:    uploadID,
		Status:   consts.TaskStatusPending,
		MaxRetry: 3,
	}
	taskID, err := srv.asyncRepo.CreateAsyncTask(ctx, task)
	if err != nil {
		return err
	}

	if err := srv.enqueueAsyncTask(ctx, taskID); err != nil {
		srv.logger.Warn("failed to enqueue async task, pending scanner will retry",
			zap.Int64("task_id", taskID),
			zap.String("task_type", task.TaskType),
			zap.Error(err))
	}
	return nil
}

func (srv *Service) enqueueAsyncTask(ctx context.Context, taskID int64) error {
	if taskID <= 0 {
		return nil
	}

	queued, err := srv.asyncRepo.MarkAsyncTaskQueued(ctx, taskID)
	if err != nil {
		return err
	}
	if !queued {
		return nil
	}

	return srv.asyncRedis.EnqueueTask(ctx, taskID)
}

func (srv *Service) AbortMultipartUpload(ctx *common.UserInfoCtx, uploadID string) common.Errno {

	upload, err := srv.multipartRepo.GetMultipartUploadByID(ctx, ctx.UserID, uploadID)
	if err != nil {
		return common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.FileUploadIdNotFound)
	}

	if upload.Status != consts.MultipartUploadStatusUploading {
		return common.ParamErr.WithMsg("multipart upload is not in uploading state")
	}

	var statusAborted int32 = consts.MultipartUploadStatusAborted
	lastActive := time.Now()
	if _, err := srv.multipartRepo.UpdateMultipartUpload(ctx, ctx.UserID, uploadID, &do.UpdateMultipartUpload{
		Status:       &statusAborted,
		LastActiveAt: &lastActive,
	}); err != nil {
		return common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	srv.rdsmultipart.DelTimeoutMultipartCancel(ctx, uploadID)

	if err = srv.publishTask(ctx, consts.TaskTypeAbortMultipart, uploadID); err != nil {
		return common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	return common.OK
}
