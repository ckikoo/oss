package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"oss/adaptor"
	"oss/consts"
	"oss/service/dto"
	"time"

	"github.com/go-redis/redis"
)

type IToken interface {
	CreateUploadToken(ctx context.Context, token string, value string, expire time.Duration) error

	CreateDownloadToken(ctx context.Context, token string, value string, expire time.Duration) error

	GetUploadToken(ctx context.Context, token string) (*dto.CreateUploadTokenReq, error)
	GetDownloadToken(ctx context.Context, token string) (*dto.CreateDownloadTokenReq, error)
}

type Token struct {
	redis *redis.Client
}

func NewToken(adaptor adaptor.IAdaptor) *Token {
	return &Token{
		redis: adaptor.GetRedis(),
	}
}

var _ IToken = (*Token)(nil)

func fmtUploadTokenKey(token string) string {
	return fmt.Sprintf("%s:upload:%s", consts.ServerName, token)
}

func fmtDownloadTokenKey(token string) string {
	return fmt.Sprintf("%s:upload:%s", consts.ServerName, token)
}

func (t *Token) CreateUploadToken(ctx context.Context, token string, value string, expire time.Duration) error {
	key := fmtUploadTokenKey(token)
	return t.redis.Set(key, value, expire).Err()
}
func (t *Token) CreateDownloadToken(ctx context.Context, token string, value string, expire time.Duration) error {
	key := fmtDownloadTokenKey(token)
	return t.redis.Set(key, value, expire).Err()
}

func (t *Token) GetUploadToken(ctx context.Context, token string) (*dto.CreateUploadTokenReq, error) {
	key := fmtUploadTokenKey(token)
	str, err := t.redis.Get(key).Result()
	if err != nil {
		return nil, err
	}

	req := &dto.CreateUploadTokenReq{}

	if err := json.Unmarshal([]byte(str), req); err != nil {
		return nil, err
	}

	return req, nil
}
func (t *Token) GetDownloadToken(ctx context.Context, token string) (*dto.CreateDownloadTokenReq, error) {
	key := fmtDownloadTokenKey(token)
	str, err := t.redis.Get(key).Result()
	if err != nil {
		return nil, err
	}

	req := &dto.CreateDownloadTokenReq{}

	if err := json.Unmarshal([]byte(str), req); err != nil {
		return nil, err
	}
	return req, nil
}
