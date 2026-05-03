package redis

import (
	"context"
	"fmt"
	"oss/adaptor"
	"oss/config"
	"time"

	"github.com/go-redis/redis"
	"github.com/gogf/gf/util/gconv"
)

type IMultipart interface {
	// 上传分片超时取消
	SetTimeoutMultipartCancel(ctx context.Context, uploadID string) error
	GetTimeWaitMultipartCancel(ctx context.Context) ([]string, error)
	DelTimeoutMultipartCancel(ctx context.Context, uploadID string) error
}

type Multipart struct {
	redis *redis.Client
}

func NewMultipart(adaptor adaptor.IAdaptor) *Multipart {
	return &Multipart{
		redis: adaptor.GetRedis(),
	}
}

var _ IMultipart = (*Multipart)(nil)

func fmtTimeoutOrderCancelZSetKey() string {
	return fmt.Sprintf("%s:mutipart:timeout:cancel", config.ServerName)
}
func (m *Multipart) SetTimeoutMultipartCancel(ctx context.Context, uploadID string) error {
	redisKey := fmtTimeoutOrderCancelZSetKey()
	_, err := m.redis.ZAdd(redisKey, redis.Z{
		Score:  float64(time.Now().UnixMilli()),
		Member: uploadID,
	}).Result()
	if err != nil {
		return err
	}
	return nil
}

func (m *Multipart) GetTimeWaitMultipartCancel(ctx context.Context) ([]string, error) {
	redisKey := fmtTimeoutOrderCancelZSetKey()

	strList, err := m.redis.ZRangeByScore(redisKey, redis.ZRangeBy{
		Min:    gconv.String(0),
		Max:    gconv.String(time.Now().UnixMilli()),
		Offset: 0,
		Count:  100,
	}).Result()
	if err != nil {
		return nil, err
	}

	return strList, nil
}

func (m *Multipart) DelTimeoutMultipartCancel(ctx context.Context, uploadID string) error {
	redisKey := fmtTimeoutOrderCancelZSetKey()
	_, err := m.redis.ZRem(redisKey, uploadID).Result()
	if err != nil {
		return err
	}
	return nil
}
