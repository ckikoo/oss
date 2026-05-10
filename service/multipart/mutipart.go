package multipart

import (
	"bytes"
	"fmt"
	"oss/adaptor"
	"oss/adaptor/redis"
	"oss/adaptor/repo/admin"
	gormAdmin "oss/adaptor/repo/admin/gorm"
	"oss/adaptor/repo/async"
	gormAsync "oss/adaptor/repo/async/gorm"
	"oss/adaptor/repo/bucket"
	gormBucket "oss/adaptor/repo/bucket/gorm"
	"oss/adaptor/repo/multipart"
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
	"oss/utils/tools"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
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
}

func NewService(adaptor adaptor.IAdaptor) *Service {
	return &Service{
		txManger:      adaptor.GetTxManager(),
		userRepo:      gormAdmin.NewUserRepo(adaptor.GetGORM()),
		objRepo:       gormObject.NewObjectRepo(adaptor.GetGORM()),
		bucketRepo:    gormBucket.NewBucketRepo(adaptor.GetGORM()),
		multipartRepo: gormMultipart.NewObjectRepo(adaptor.GetGORM()),
		rdsmultipart:  redis.NewMultipart(adaptor),
		storage:       adaptor.GetStorage(),
		asyncRepo:     gormAsync.NewAsyncTaskRepo(adaptor.GetGORM()),
		asyncRedis:    redis.NewTask(adaptor),
		eventService:  event.NewService(adaptor),
		tokenRedis:    redis.NewToken(adaptor),
	}
}
func (srv *Service) CreateMultipartUpload(ctx *common.UserInfoCtx, bucketName string, req *dto.CreateMultipartUploadReq) (*dto.CreateMultipartUploadResp, common.Errno) {
	if bucketName == "" || req.ObjectKey == "" {
		return nil, common.ParamErr.WithMsg("bucket_name and object_key are required")
	}

	if req.TotalChunk <= 0 {
		return nil, common.ParamErr.WithMsg("total_chunk must greate zero")
	}

	bucket, err := srv.bucketRepo.GetByName(ctx, ctx.UserID, bucketName)
	if err != nil {
		return nil, common.DatabaseErr.WithMsg("bucket not found")
	}

	temp, err := srv.objRepo.GetByKey(ctx, bucketName, req.ObjectKey, "")
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, common.DatabaseErr.WithErr(err)
	}

	if temp != nil && req.Overwrite == false {
		return nil, common.FileNameExists
	}

	uInfo, err := srv.userRepo.GetUserInfoById(ctx, ctx.UserID)
	if err != nil {
		return nil, common.DatabaseErr.WithErr(err)
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
		return nil, common.DatabaseErr.WithErr(err)
	}

	err = srv.rdsmultipart.SetTimeoutMultipartCancel(ctx, uploadID, createUpload.ExpiresAt)
	if err != nil {
		return nil, common.DatabaseErr.WithErr(err)
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

	limitStr, err := srv.tokenRedis.GetUploadTokenFields(ctx, token, redis.FieldSizeLimit)
	if err != nil {
		return nil, common.RedisErr.WithErr(err)
	}

	if limitStr[redis.FieldSizeLimit] != "" {
		limit, err := strconv.ParseInt(limitStr[redis.FieldSizeLimit], 10, 64)
		if err != nil {
			return nil, common.ErrInternalServer.WithErr(err)
		}

		if int64(len(data)) > limit {
			return nil, common.ParamErr.WithMsg(fmt.Sprintf("part size exceeds limit of %d bytes", limit))
		}
	}

	upload, err := srv.multipartRepo.GetMultipartUploadByID(ctx, ctx.UserID, uploadID)
	if err != nil {
		return nil, common.ParamErr.WithErr(err)
	}

	uinfo, err := srv.userRepo.GetUserInfoById(ctx, ctx.UserID)
	if err != nil {
		return nil, common.DatabaseErr.WithErr(err)
	}
	// 只有在上传中状态才允许上传分片，已完成、已中止的上传不允许再上传分片
	if upload.Status != consts.MultipartPartStatusUploading {
		return nil, common.ParamErr.WithMsg("multipart upload is not in uploading state")
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
		return nil, common.DatabaseErr.WithErr(err)
	}

	lastActive := time.Now()
	update := &do.UpdateMultipartUpload{LastActiveAt: &lastActive}

	if _, err := srv.multipartRepo.UpdateMultipartUpload(ctx, ctx.UserID, uploadID, update); err != nil {
		return nil, common.DatabaseErr.WithErr(err)
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
func (srv *Service) CompleteMultipartUpload(ctx *common.UserInfoCtx, uploadID string, req *dto.CompleteMultipartUploadReq) (*dto.CompleteMultipartUploadResp, common.Errno) {
	if uploadID == "" {
		return nil, common.ParamErr.WithMsg("upload_id are required")
	}

	if len(req.Parts) == 0 {
		return nil, common.ParamErr.WithMsg("parts are required")
	}

	upload, err := srv.multipartRepo.GetMultipartUploadByID(ctx, ctx.UserID, uploadID)
	if err != nil {
		return nil, common.ParamErr.WithErr(err)
	}

	uInfo, err := srv.userRepo.GetUserInfoById(ctx, ctx.UserID)
	if err != nil {
		return nil, common.DatabaseErr.WithErr(err)
	}

	if upload.Status != consts.MultipartUploadStatusUploading {
		return nil, common.FileHasUploadSuccess
	}

	storedParts, err := srv.multipartRepo.ListMultipartParts(ctx, ctx.UserID, uploadID)
	if err != nil {
		return nil, common.DatabaseErr.WithErr(err)
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
		VersionID:     "",
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
		return nil, common.DatabaseErr.WithErr(err)
	}

	srv.rdsmultipart.DelTimeoutMultipartCancel(ctx, uploadID)

	if err := srv.publishTask(ctx, consts.TaskTypePhysicalMerge, uploadID, objectID); err != nil {
		return nil, common.DatabaseErr.WithErr(err)
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
		VersionID: "",
		Status:    statusMerged,
	}, common.OK
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
		return common.DatabaseErr.WithErr(err)
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
		return common.DatabaseErr.WithErr(err)
	}

	srv.rdsmultipart.DelTimeoutMultipartCancel(ctx, uploadID)

	if err = srv.publishTask(ctx, consts.TaskTypeAbortMultipart, uploadID, 0); err != nil {
		return common.DatabaseErr.WithErr(err)
	}

	return common.OK
}
