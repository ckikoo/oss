package token

import (
	"context"
	"fmt"
	"oss/adaptor"
	"oss/adaptor/redis"
	"oss/adaptor/repo/accesskey"
	"oss/adaptor/repo/bucket"
	"oss/common"
	"oss/consts"
	"oss/service/dto"
	"oss/utils/tools"
	"strings"
	"time"

	"github.com/gogf/gf/util/gconv"
	"gorm.io/gorm"
)

type Service struct {
	adaptor adaptor.IAdaptor
	bucket  bucket.IBucketRepo
	access  accesskey.IAccessKeyRepo
	rds     redis.IToken
}

func NewService(adaptor adaptor.IAdaptor) *Service {
	return &Service{
		adaptor: adaptor,
		bucket:  bucket.NewBucketRepo(adaptor),
		access:  accesskey.NewAccessKeyRepo(adaptor),
		rds:     redis.NewToken(adaptor),
	}
}

func (s *Service) CreateUploadToken(ctx context.Context, ak string, secure string, req *dto.CreateUploadTokenReq) (*dto.CreateTokenResp, common.Errno) {
	expireAt := time.Now().Add(time.Duration(req.ExpiresIn) * time.Second)
	expireAtUnix := expireAt.Unix()

	bucketInfo, err := s.bucket.GetByName(ctx, req.BucketName)
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, common.DatabaseErr.WithErr(err)
	}

	if bucketInfo == nil {
		return nil, common.ParamErr.WithMsg("bucket not exist")
	}

	token := genToken(ak, req.BucketName, req.ObjectKey, consts.UploadMethod, consts.UploadAction, expireAtUnix, secure)

	s.rds.CreateUploadToken(ctx, token, gconv.String(req), time.Duration(req.ExpiresIn)*time.Second)
	return &dto.CreateTokenResp{
		Token:    token,
		ExpireAt: expireAtUnix,
	}, common.OK
}

func (s *Service) CreateDownloadToken(ctx context.Context, ak string, secure string, req *dto.CreateDownloadTokenReq) (*dto.CreateTokenResp, common.Errno) {
	expireAt := time.Now().Add(time.Duration(req.ExpiresIn) * time.Second)
	expireAtUnix := expireAt.Unix()
	bucketInfo, err := s.bucket.GetByName(ctx, req.BucketName)
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, common.DatabaseErr.WithErr(err)
	}

	if bucketInfo == nil {
		return nil, common.ParamErr.WithMsg("bucket not exist")
	}

	token := genToken(ak, req.BucketName, req.ObjectKey, consts.DownloadMethod, consts.DownloadAction, expireAtUnix, secure)

	s.rds.CreateDownloadToken(ctx, token, gconv.String(req), time.Duration(req.ExpiresIn)*time.Second)

	return &dto.CreateTokenResp{
		Token:    token,
		ExpireAt: expireAtUnix,
	}, common.OK
}

func (s *Service) ValidateToken(ctx context.Context, token string, action string) (ak string, pass bool) {
	// 从redis获取token信息，判断是否存在

	switch action {
	case consts.UploadAction:
		req, err := s.rds.GetUploadToken(ctx, token)
		if err != nil || req == nil {
			return "", false
		}
	case consts.DownloadAction:
		req, err := s.rds.GetDownloadToken(ctx, token)
		if err != nil || req == nil {
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

	expire := time.Now().Add(time.Duration(time.Unix(ExpiresIn, 0).Second())).UnixMilli()

	sb.WriteString(gconv.String(expire))

	token := tools.HmacSHA256(sb.String(), secure)

	return fmt.Sprintf("%s:%s:%d", ak, token, expire)
}
