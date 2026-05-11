package token

import (
	"context"
	"fmt"
	"oss/adaptor"
	"oss/adaptor/redis"
	"oss/adaptor/repo/accesskey"
	gormAccessKey "oss/adaptor/repo/accesskey/gorm"
	"oss/adaptor/repo/bucket"
	gormBucket "oss/adaptor/repo/bucket/gorm"
	"oss/common"
	"oss/consts"
	"oss/service/dto"
	"oss/utils/logger"
	"oss/utils/tools"
	"strings"
	"time"

	"github.com/gogf/gf/util/gconv"
	"go.uber.org/zap"
)

type Service struct {
	adaptor adaptor.IAdaptor
	bucket  bucket.IBucketRepo
	access  accesskey.IAccessKeyRepo
	rds     redis.IToken
	logger  *zap.Logger
}

func NewService(adaptor adaptor.IAdaptor) *Service {
	return &Service{
		adaptor: adaptor,
		bucket:  gormBucket.NewBucketRepo(adaptor),
		access:  gormAccessKey.NewAccessKeyRepo(adaptor.GetGORM()),
		rds:     redis.NewToken(adaptor),
		logger:  logger.GetLogger().With(zap.String("module", "token")),
	}
}

func (s *Service) CreateUploadToken(ctx *common.UserInfoCtx, req *dto.CreateUploadTokenReq) (*dto.CreateTokenResp, common.Errno) {
	expireAt := time.Now().Add(time.Duration(req.ExpiresIn) * time.Second)
	expireAtUnix := expireAt.Unix()

	accessInfo, err := s.access.GetByAccessKey(ctx, ctx.AccessKey)
	if err != nil {
		s.logger.Error("CreateUploadToken failed to get access key", zap.Error(err), zap.String("access_key", ctx.AccessKey))
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	_, err = s.bucket.GetByName(ctx, accessInfo.UserID, req.BucketName)
	if err != nil {
		s.logger.Error("CreateUploadToken failed to get bucket", zap.Error(err), zap.String("bucket_name", req.BucketName))
		return nil, common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.BucketNotFoundErr)
	}

	token := genToken(ctx.AccessKey, req.BucketName, req.ObjectKey, consts.UploadMethod, consts.UploadAction, expireAtUnix, ctx.SecretKey)

	req.ExpiresIn = expireAtUnix
	if err := s.rds.CreateUploadToken(ctx, token, (req), time.Duration(req.ExpiresIn)*time.Second); err != nil {
		return nil, common.RedisErr.WithErr(err)
	}

	return &dto.CreateTokenResp{
		Token:    token,
		ExpireAt: expireAtUnix,
	}, common.OK
}

func (s *Service) CreateDownloadToken(ctx *common.UserInfoCtx, req *dto.CreateDownloadTokenReq) (*dto.CreateTokenResp, common.Errno) {
	expireAt := time.Now().Add(time.Duration(req.ExpiresIn) * time.Second)
	expireAtUnix := expireAt.Unix()

	bucketInfo, err := s.bucket.GetByName(ctx, ctx.UserID, req.BucketName)
	if err != nil {
		s.logger.Error("CreateDownloadToken failed to get bucket", zap.Error(err), zap.String("bucket_name", req.BucketName))
		return nil, common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.BucketNotFoundErr)
	}

	if bucketInfo == nil {
		return nil, common.BucketNotFoundErr
	}

	token := genToken(ctx.AccessKey, req.BucketName, req.ObjectKey, consts.DownloadMethod, consts.DownloadAction, expireAtUnix, ctx.SecretKey)

	req.ExpiresIn = expireAtUnix
	if err := s.rds.CreateDownloadToken(ctx, token, (req), time.Duration(req.ExpiresIn)*time.Second); err != nil {
		return nil, common.RedisErr.WithErr(err)
	}

	return &dto.CreateTokenResp{
		Token:    token,
		ExpireAt: expireAtUnix,
	}, common.OK
}

func (s *Service) ValidateToken(ctx context.Context, token string, action string, expectedBucketName, expectedObjectKey string) (ak string, pass bool) {
	// 从redis获取token信息，判断是否存在

	switch action {
	case consts.UploadAction:
		req, err := s.rds.GetUploadToken(ctx, token)
		if err != nil || req == nil {
			return "", false
		}
		if expectedBucketName != "" && req.BucketName != expectedBucketName {
			return "", false
		}
		if expectedObjectKey != "" && req.ObjectKey != expectedObjectKey {
			return "", false
		}
	case consts.DownloadAction:
		req, err := s.rds.GetDownloadToken(ctx, token)
		if err != nil || req == nil {
			return "", false
		}
		if expectedBucketName != "" && req.BucketName != expectedBucketName {
			return "", false
		}
		if expectedObjectKey != "" && req.ObjectKey != expectedObjectKey {
			return "", false
		}
	}

	tokenParts := strings.SplitN(token, ":", 3)
	if len(tokenParts) != 3 {
		return "", false
	}

	ak = tokenParts[0]

	return ak, true
}

// ExpiresIn单位为秒 0 标识永不过期
func genToken(ak string, bucket, object, method, action string, ExpiresIn int64, secure string) string {
	sb := strings.Builder{}

	sb.WriteString(bucket)
	sb.WriteString(":")
	sb.WriteString(object)
	sb.WriteString(":")
	sb.WriteString(method)
	sb.WriteString(":")
	sb.WriteString(action)
	sb.WriteString(":")

	sb.WriteString(gconv.String(ExpiresIn))

	token := tools.HmacSHA256(sb.String(), secure)

	return fmt.Sprintf("%s:%s:%d", ak, token, ExpiresIn)
}
