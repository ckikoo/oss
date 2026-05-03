package mutipart

import (
	"context"
	"fmt"
	"mime/multipart"
	"os"
	"oss/adaptor"
	"oss/adaptor/redis"
	"oss/adaptor/repo/bucket"
	mutipartRepo "oss/adaptor/repo/multipart"
	"oss/adaptor/repo/object"
	"oss/common"
	"oss/consts"
	"oss/service/do"
	"oss/service/dto"
	"oss/utils/tools"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	objRepo       object.IObjectRepo
	multipartRepo mutipartRepo.IMultipartRepo
	bucketRepo    bucket.IBucketRepo
	rdsMutipart   redis.IMultipart
}

func NewService(adaptor adaptor.IAdaptor) *Service {
	return &Service{
		bucketRepo:    bucket.NewBucketRepo(adaptor),
		multipartRepo: mutipartRepo.NewObjectRepo(adaptor),
		rdsMutipart:   redis.NewMultipart(adaptor),
	}
}
func (srv *Service) CreateMultipartUpload(ctx context.Context, user_id int64, bucketName string, req *dto.CreateMultipartUploadReq) (*dto.CreateMultipartUploadResp, common.Errno) {
	if bucketName == "" || req.ObjectKey == "" {
		return nil, common.ParamErr.WithMsg("bucket_name and object_key are required")
	}

	if req.TotalChunk < 0 {
		return nil, common.ParamErr.WithMsg("total_chunk cannot be negative")
	}

	bucket, err := srv.bucketRepo.GetByName(ctx, bucketName)
	if err != nil {
		return nil, common.DatabaseErr.WithMsg("bucket not found")
	}

	if bucket.UserID != user_id {
		return nil, common.AuthErr
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
		Metadata:      &req.Metadata,
		ExpiresAt:     time.Now().Add(24 * time.Hour),
		LastActiveAt:  time.Now(),
	}
	defer func() {
		if err != nil {
			srv.rdsMutipart.DelTimeoutMultipartCancel(ctx, uploadID)
		}
	}()

	if _, err = srv.multipartRepo.CreateMultipartUpload(ctx, createUpload); err != nil {
		return nil, common.DatabaseErr.WithErr(err)
	}

	err = srv.rdsMutipart.SetTimeoutMultipartCancel(ctx, uploadID)
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

func (srv *Service) UploadMultipartPart(ctx context.Context, user_id int64, upload_etag string, uploadID string, partNumber int32, file *multipart.FileHeader) (*dto.UploadMultipartPartResp, common.Errno) {
	if uploadID == "" || partNumber <= 0 {
		return nil, common.ParamErr.WithMsg("upload_id and part_number are required")
	}

	upload, err := srv.multipartRepo.GetMultipartUploadByID(ctx, uploadID)
	if err != nil {
		return nil, common.ParamErr.WithErr(err)
	}

	// 校验上传权限和上传状态
	if upload.UserID != user_id {
		return nil, common.AuthErr
	}

	// 只有在上传中状态才允许上传分片，已完成、已中止的上传不允许再上传分片
	if upload.Status != consts.MultipartUploadStatusUploading {
		return nil, common.ParamErr.WithMsg("multipart upload is not in uploading state")
	}

	f, err := file.Open()
	if err != nil {
		return nil, common.ServerErr.WithErr(err)
	}
	defer f.Close()

	storagePath := fmt.Sprintf("/storage/%s/multipart/%s/part_%d", upload.BucketName, uploadID, partNumber)
	storageDir := filepath.Dir(storagePath)
	if err := os.MkdirAll(storageDir, consts.FilePermDir); err != nil {
		return nil, common.ServerErr.WithErr(err)
	}

	etag, _, size, err := tools.SaveFileAndComputeHashes(f, storagePath)
	if err != nil {
		return nil, common.ServerErr.WithErr(err)
	}

	if etag != upload_etag {
		return nil, common.FileCheckErr
	}

	part := &do.CreateMultipartPart{
		UploadID:    uploadID,
		PartNumber:  partNumber,
		Size:        size,
		Etag:        etag,
		StoragePath: storagePath,
		Status:      consts.MultipartPartStatusConfirmed,
	}

	_, err = srv.multipartRepo.CreateOrUpdateMultipartPart(ctx, part)
	if err != nil {
		return nil, common.DatabaseErr.WithErr(err)
	}

	lastActive := time.Now()
	update := &do.UpdateMultipartUpload{LastActiveAt: &lastActive}

	if upload.TotalChunk < partNumber {
		totalChunk := partNumber
		update.TotalChunk = &totalChunk
	}

	if _, err := srv.multipartRepo.UpdateMultipartUpload(ctx, uploadID, update); err != nil {
		return nil, common.DatabaseErr.WithErr(err)
	}

	return &dto.UploadMultipartPartResp{
		PartNumber: partNumber,
		Etag:       etag,
		Size:       size,
		Status:     consts.MultipartPartStatusConfirmed,
	}, common.OK
}

// 不做真正的合并逻辑 做的是伪合并逻辑
func (srv *Service) CompleteMultipartUpload(ctx context.Context, userId int64, uploadID string, req *dto.CompleteMultipartUploadReq) (*dto.CompleteMultipartUploadResp, common.Errno) {
	if uploadID == "" {
		return nil, common.ParamErr.WithMsg("upload_id are required")
	}
	if len(req.Parts) == 0 {
		return nil, common.ParamErr.WithMsg("parts are required")
	}

	upload, err := srv.multipartRepo.GetMultipartUploadByID(ctx, uploadID)
	if err != nil {
		return nil, common.ParamErr.WithErr(err)
	}

	if upload.UserID != userId {
		return nil, common.AuthErr
	}

	if upload.Status != consts.MultipartUploadStatusUploading {
		return nil, common.FileHasUploadSuccess
	}

	storedParts, err := srv.multipartRepo.ListMultipartParts(ctx, uploadID)
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
	metadata := ""
	if upload.Metadata != nil {
		metadata = *upload.Metadata
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
		Metadata:      &metadata,
	})
	if err != nil {
		return nil, common.DatabaseErr.WithErr(err)
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

	if _, err := srv.multipartRepo.UpdateMultipartUpload(ctx, uploadID, update); err != nil {
		return nil, common.DatabaseErr.WithErr(err)
	}

	srv.rdsMutipart.DelTimeoutMultipartCancel(ctx, uploadID)

	return &dto.CompleteMultipartUploadResp{
		ObjectID:  objectID,
		ObjectKey: upload.ObjectKey,
		VersionID: "",
		Status:    statusMerged,
	}, common.OK
}

func (srv *Service) AbortMultipartUpload(ctx context.Context, user_id int64, uploadID string) common.Errno {
	if uploadID == "" {
		return common.ParamErr.WithMsg("bucket_name and upload_id are required")
	}

	upload, err := srv.multipartRepo.GetMultipartUploadByID(ctx, uploadID)
	if err != nil {
		return common.ParamErr.WithErr(err)
	}

	if upload.UserID != user_id {
		return common.AuthErr
	}

	if upload.Status != consts.MultipartUploadStatusUploading {
		return common.ParamErr.WithMsg("multipart upload is not in uploading state")
	}

	var statusAborted int32 = consts.MultipartUploadStatusAborted
	lastActive := time.Now()
	if _, err := srv.multipartRepo.UpdateMultipartUpload(ctx, uploadID, &do.UpdateMultipartUpload{
		Status:       &statusAborted,
		LastActiveAt: &lastActive,
	}); err != nil {
		return common.DatabaseErr.WithErr(err)
	}

	if err := srv.multipartRepo.DeleteMultipartParts(ctx, uploadID); err != nil {
		return common.DatabaseErr.WithErr(err)
	}
	_ = os.RemoveAll(fmt.Sprintf("/storage/%s/multipart/%s", upload.BucketName, uploadID))
	srv.rdsMutipart.DelTimeoutMultipartCancel(ctx, uploadID)

	return common.OK
}
