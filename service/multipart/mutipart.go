package multipart

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"oss/adaptor"
	"oss/adaptor/redis"
	"oss/adaptor/repo/admin"
	gormAdmin "oss/adaptor/repo/admin/gorm"
	"oss/adaptor/repo/async"
	gormAsync "oss/adaptor/repo/async/gorm"
	"oss/adaptor/repo/bucket"
	gormBucket "oss/adaptor/repo/bucket/gorm"
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
	"oss/consts"
	"oss/service/do"
	"oss/service/dto"
	"oss/service/event"
	"oss/utils/logger"
	"oss/utils/tools"
	"sort"
	"strconv"
	"strings"
	"time"

	"encoding/json"
	"io"
	"net/http"

	"github.com/gogf/gf/util/gconv"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
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
}

func NewService(adaptor adaptor.IAdaptor) *Service {
	return &Service{
		txManger:      adaptor.GetTxManager(),
		userRepo:      gormAdmin.NewUserRepo(adaptor.GetGORM()),
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
		logger:        logger.GetLogger().With(zap.String("module", "multipart")),
	}
}
func (srv *Service) CreateMultipartUpload(ctx *common.UserInfoCtx, bucketName string, req *dto.CreateMultipartUploadReq) (*dto.CreateMultipartUploadResp, common.Errno) {

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

	uploadID := uuid.NewString()
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

func (srv *Service) UploadMultipartPart(ctx *common.UserInfoCtx, token string, upload_etag string, uploadID string, partNumber int32, data []byte) (*dto.UploadMultipartPartResp, common.Errno) {

	limitMap, err := srv.tokenRedis.GetUploadTokenFields(ctx, token, redis.FieldSizeLimit, redis.FieldBucketName, redis.FieldObjectKey, redis.FieldExpiresIn)
	if err != nil {
		return nil, common.RedisErr.WithErr(err)
	}

	if limitMap[redis.FieldExpiresIn] != "" {
		expiresIn, err := strconv.ParseInt(limitMap[redis.FieldExpiresIn], 10, 64)
		if err != nil {
			return nil, common.ErrInternalServer.WithErr(err)
		}

		if time.Now().Unix() > expiresIn {
			return nil, common.TokenExpired
		}
	}

	bucketName := limitMap[redis.FieldBucketName]

	bucket, err := srv.bucketRepo.GetByName(ctx, ctx.UserID, bucketName)
	if err != nil {
		srv.logger.Error("UploadMultipartPart bucketRepo.GetByName error", zap.Error(err), zap.String("redis json", gconv.String(limitMap)))
	}

	if bucket != nil {
		if err := srv.meteringRepo.UpdateDailyMetrics(ctx, bucket.UserID, &bucket.ID, time.Now(), 0, 0, int64(len(data)), 0, 1, 0, 0); err != nil {
			srv.logger.Error("UploadMultipartPart meteringRepo.UpdateDailyMetrics error", zap.Error(err), zap.String("redis json", gconv.String(limitMap)))
		}
	}

	if limitMap[redis.FieldSizeLimit] != "" {
		limit, err := strconv.ParseInt(limitMap[redis.FieldSizeLimit], 10, 64)
		if err != nil {
			return nil, common.ErrInternalServer.WithErr(err)
		}

		if int64(len(data)) > limit {
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

	uinfo, err := srv.userRepo.GetUserInfoById(ctx, ctx.UserID)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	// 只有在上传中状态才允许上传分片，已完成、已中止的上传不允许再上传分片
	if upload.Status != consts.MultipartPartStatusUploading {
		return nil, common.FileUploadIdStatusNotOnUpload
	}

	if partNumber <= 0 || partNumber > upload.TotalChunk {
		return nil, common.ParamErr.WithMsg("part_number exceeds total_chunk")
	}

	dataSize := int64(len(data))
	if uinfo.StorageQuota > 0 && uinfo.StorageUsed+dataSize > uinfo.StorageQuota {
		return nil, common.StorageQuotaOver
	}

	res, err := srv.storage.PutPart(ctx, upload.BucketName, uploadID, partNumber, bytes.NewReader(data))
	if err != nil {
		return nil, common.ServerErr.WithErr(err)
	}

	if res.Etag != upload_etag {
		srv.storage.DeletePart(ctx, upload.BucketName, uploadID, partNumber)
		return nil, common.FileCheckErr
	}

	part := &do.CreateMultipartPart{
		UploadID:    uploadID,
		PartNumber:  partNumber,
		Size:        res.Size,
		Etag:        res.Etag,
		StoragePath: res.StoragePath,
		Status:      consts.MultipartPartStatusConfirmed,
	}

	_, err = srv.multipartRepo.CreateOrUpdateMultipartPart(ctx, part)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	lastActive := time.Now()
	update := &do.UpdateMultipartUpload{LastActiveAt: &lastActive}

	if _, err := srv.multipartRepo.UpdateMultipartUpload(ctx, ctx.UserID, uploadID, update); err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	return &dto.UploadMultipartPartResp{
		PartNumber: partNumber,
		Etag:       res.Etag,
		Size:       res.Size,
		Status:     consts.MultipartPartStatusConfirmed,
	}, common.OK
}

// TODO 回调机制，以及 允许覆盖策略

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

	bucket, err := srv.bucketRepo.GetByName(ctx, ctx.UserID, bucketName)
	if err != nil {
		return nil, common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.BucketNotFoundErr)
	}

	oldObject, err := srv.objRepo.GetByKey(ctx, bucketName, upload.ObjectKey, "")
	if err != nil && !errors.Is(err, repoerr.ErrNotFound) {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	switch bucket.Versioning {
	// 版本控制开启，允许同名覆盖，生成删除旧的版本
	case consts.BucketVersioningDisabled:
		if oldObject != nil {
			// 删除旧版本的存储文件 只有分片需要删除，别的直接覆盖
			if oldObject.IsMultipart == consts.ObjectIsMultipartMerged {
				err := srv.storage.DeleteParts(ctx, oldObject.BucketName, *oldObject.UploadID)
				if err != nil {
					srv.logger.Warn("failed to delete old multipart parts storage",
						zap.String("bucket_name", oldObject.BucketName),
						zap.String("upload_id", *oldObject.UploadID),
						zap.Error(err))
				}

				if err := srv.txManger.RunInTx(ctx, func(ctx1 context.Context, tx tx.Tx) error {
					// 删除旧版本的数据库记录
					err = srv.objRepo.DeleteObject(ctx1, oldObject.BucketName, oldObject.ObjectKey, oldObject.VersionID)
					if err != nil {
						srv.logger.Warn("failed to delete old object record",
							zap.String("bucket_name", oldObject.BucketName),
							zap.String("object_key", oldObject.ObjectKey),
							zap.String("version_id", oldObject.VersionID),
							zap.Error(err))
						return err
					}

					return srv.userRepo.UpdateStorageUsed(ctx1, ctx.UserID, -oldObject.Size)
				}); err != nil {
					return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
				}
			}
		}
	}

	createReqMap, err := srv.tokenRedis.GetUploadTokenFields(ctx, uploadID, redis.FieldCallbackURL)
	if err != nil {
		return nil, common.RedisErr.WithErr(err)
	}

	uInfo, err := srv.userRepo.GetUserInfoById(ctx, ctx.UserID)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
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

	if uInfo.StorageQuota != 0 && uInfo.StorageUsed+totalSize > uInfo.StorageQuota {
		srv.AbortMultipartUpload(ctx, uploadID)
		return nil, common.StorageQuotaOver
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

	var statusMerged int32 = consts.MultipartPartStatusVirtualMerge
	lastActive := time.Now()
	update := &do.UpdateMultipartUpload{
		Status:       &statusMerged,
		LastActiveAt: &lastActive,
	}
	if upload.TotalChunk > 0 {
		totalChunk := upload.TotalChunk
		update.TotalChunk = &totalChunk
	}

	objectID, err := srv.objRepo.CreateObject(ctx, &do.CreateObject{
		BucketID:      upload.BucketID,
		BucketName:    upload.BucketName,
		ObjectKey:     upload.ObjectKey,
		ObjectKeyHash: upload.ObjectKeyHash,
		VersionID:     uploadID,
		Size:          totalSize,
		Etag:          resultEtag,
		ContentType:   &contentType,
		StorageClass:  storageClass,
		IsMultipart:   consts.ObjectIsMultipartMerged,
		UploadID:      &uploadID,
		Acl:           consts.ObjectAclInheritBucket,
		Metadata:      metadata,
		CallBack: func(tx *gorm.DB) error {
			if _, err := srv.multipartRepo.WithTx(tx).UpdateMultipartUpload(ctx, ctx.UserID, uploadID, update); err != nil {
				return err
			}
			return srv.userRepo.WithTx(tx).UpdateStorageUsed(ctx, ctx.UserID, totalSize)
		},
	})
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	srv.rdsmultipart.DelTimeoutMultipartCancel(ctx, uploadID)

	if err := srv.publishTask(ctx, consts.TaskTypePhysicalMerge, uploadID, objectID); err != nil {
		srv.logger.Error("failed to publish physical merge task", zap.String("upload_id", uploadID), zap.Int64("object_id", objectID), zap.Error(err))
	}

	// 回调事件
	if createReqMap[redis.FieldCallbackURL] != "" {
		callbackURL := createReqMap[redis.FieldCallbackURL]
		callbackPayload := map[string]interface{}{
			"event_type":  "multipart_complete",
			"bucket_name": upload.BucketName,
			"object_key":  upload.ObjectKey,
			"upload_id":   uploadID,
			"object_id":   objectID,
			"version_id":  uploadID,
			"size":        totalSize,
			"etag":        resultEtag,
			"parts_count": len(sortedParts),
			"status":      "completed",
		}
		go srv.sendMultipartCompleteCallback(callbackURL, callbackPayload)
	}

	// 触发事件
	go srv.eventService.TriggerEvent(ctx, upload.BucketID, consts.EventTypeMultipartComplete, upload.ObjectKey, map[string]interface{}{
		"bucket_name": upload.BucketName,
		"object_key":  upload.ObjectKey,
		"upload_id":   uploadID,
		"size":        totalSize,
		"etag":        resultEtag,
		"parts_count": len(sortedParts),
	})

	return &dto.CompleteMultipartUploadResp{
		ObjectID:  objectID,
		ObjectKey: upload.ObjectKey,
		VersionID: uploadID,
		Status:    statusMerged,
	}, common.OK
}

func (srv *Service) sendMultipartCompleteCallback(callbackURL string, payload map[string]interface{}) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		srv.logger.Error("failed to marshal multipart callback payload", zap.Error(err), zap.String("callback_url", callbackURL))
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, callbackURL, bytes.NewReader(bodyBytes))
	if err != nil {
		srv.logger.Error("failed to create multipart callback request", zap.Error(err), zap.String("callback_url", callbackURL))
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		srv.logger.Error("multipart callback request failed", zap.Error(err), zap.String("callback_url", callbackURL))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		srv.logger.Error("multipart callback returned non-success status",
			zap.String("callback_url", callbackURL),
			zap.Int("status", resp.StatusCode),
			zap.String("response", string(respBody)))
		return
	}

	srv.logger.Info("multipart callback delivered successfully", zap.String("callback_url", callbackURL), zap.String("upload_id", fmt.Sprintf("%v", payload["upload_id"])))
}

func (srv *Service) publishTask(ctx *common.UserInfoCtx, taskType string, uploadID string, objectID int64) error {
	if !consts.ValidAsyncTaskType(taskType) {
		return fmt.Errorf("invalid async task type: %s", taskType)
	}

	// 1. 先写 MySQL（持久化保证）
	task := &do.CreateAsyncTask{
		UserId:   ctx.UserID,
		TaskID:   uuid.NewString(),
		TaskType: taskType,
		UploadID: uploadID,
		ObjectID: objectID,
		Status:   consts.TaskStatusPending,
		MaxRetry: 3,
	}
	if _, err := srv.asyncRepo.CreateAsyncTask(ctx, task); err != nil {
		return err
	}

	// 2. 再推 Redis（加速消费，失败不影响正确性）
	_ = srv.asyncRedis.EnqueueTask(ctx, task.TaskID)
	// 忽略 Redis 错误，兜底扫描会补偿
	return nil
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

	if err = srv.publishTask(ctx, consts.TaskTypeAbortMultipart, uploadID, 0); err != nil {
		return common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	return common.OK
}
