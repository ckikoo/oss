package presigned

import (
	"context"
	"fmt"
	"strings"
	"time"

	"oss/adaptor"
	bucketRepo "oss/adaptor/repo/bucket"
	presignedRepo "oss/adaptor/repo/presigned"
	"oss/common"
	"oss/service/do"
	"oss/service/dto"
	"oss/utils/tools"
)

type Service struct {
	repo       presignedRepo.IPresignedRepo
	bucketRepo bucketRepo.IBucketRepo
}

func NewService(adaptor adaptor.IAdaptor) *Service {
	return &Service{
		repo:       presignedRepo.NewPresignedRepo(adaptor),
		bucketRepo: bucketRepo.NewBucketRepo(adaptor),
	}
}

func (srv *Service) CreatePresignedUrl(ctx context.Context, userID int64, req *dto.CreatePresignedUrlReq) (*dto.CreatePresignedUrlResp, common.Errno) {
	if strings.TrimSpace(req.BucketName) == "" {
		return nil, common.ParamErr.WithMsg("bucket_name is required")
	}
	if strings.TrimSpace(req.ObjectKey) == "" {
		return nil, common.ParamErr.WithMsg("object_key is required")
	}
	if strings.TrimSpace(req.Method) == "" {
		return nil, common.ParamErr.WithMsg("method is required")
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method != "GET" && method != "PUT" {
		return nil, common.ParamErr.WithMsg("method must be GET or PUT")
	}
	if req.ExpiresIn <= 0 {
		return nil, common.ParamErr.WithMsg("expires_in must be positive")
	}

	bucket, err := srv.bucketRepo.GetByName(ctx, req.BucketName)
	if err != nil {
		return nil, common.ParamErr.WithErr(err)
	}

	token, err := tools.GenerateRandomKey(16)
	if err != nil {
		return nil, common.ServerErr.WithErr(err)
	}

	expiresAt := time.Now().Add(time.Duration(req.ExpiresIn) * time.Second)
	objectKeyHash := tools.Md5Hash(req.ObjectKey)

	presigned := &do.CreatePresignedURL{
		Token:         token,
		BucketID:      bucket.ID,
		ObjectKey:     req.ObjectKey,
		ObjectKeyHash: objectKeyHash,
		Method:        method,
		SingleUse:     req.SingleUse,
		UserID:        userID,
		ExpiresAt:     expiresAt,
	}

	if err := srv.repo.CreatePresignedURL(ctx, presigned); err != nil {
		return nil, common.DatabaseErr.WithErr(err)
	}

	url := fmt.Sprintf("/api/v1/presigned-urls/%s", token)
	return &dto.CreatePresignedUrlResp{
		Token:     token,
		URL:       url,
		ExpiresAt: expiresAt.UnixMilli(),
		Method:    method,
		SingleUse: req.SingleUse,
	}, common.OK
}

func (srv *Service) RevokePresignedUrl(ctx context.Context, userID int64, token string) common.Errno {
	if token == "" {
		return common.ParamErr.WithMsg("token is required")
	}

	found, err := srv.repo.GetPresignedURLByToken(ctx, token)
	if err != nil {
		return common.ParamErr.WithErr(err)
	}
	if found.UserID != userID {
		return common.PermissionErr.WithMsg("not authorized to revoke this token")
	}

	if err := srv.repo.DeletePresignedURL(ctx, token); err != nil {
		return common.DatabaseErr.WithErr(err)
	}

	return common.OK
}
