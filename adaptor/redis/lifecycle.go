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

type ILifecycle interface {
	// 添加生命周期事件到Redis
	SetLifecycleEvent(ctx context.Context, bucketID int64, ruleID int64, prefix string, operation string, objectKey string, executeTime time.Time) error
	// 获取待执行的生命周期事件
	GetPendingLifecycleEvents(ctx context.Context, bucketID int64, ruleID int64, prefix string, operation string) ([]string, error)
	// 删除已处理的生命周期事件
	DelLifecycleEvent(ctx context.Context, bucketID int64, ruleID int64, prefix string, operation string, objectKey string) error
}

type lifeCycle struct {
	redis *redis.Client
}

var _ ILifecycle = (*lifeCycle)(nil)

func getRedisKey(bucketID int64, ruleID int64, prefix string, operation string) string {
	if prefix == "" {
		prefix = "*"
	}
	return fmt.Sprintf("%s:lifecycle:%d:%d:%s:%s", config.ServerName, bucketID, ruleID, prefix, operation)
}

func NewLifecycle(adaptor adaptor.IAdaptor) *lifeCycle {
	return &lifeCycle{
		redis: adaptor.GetRedis(),
	}
}

func (l *lifeCycle) SetLifecycleEvent(ctx context.Context, bucketID int64, ruleID int64, prefix string, operation string, objectKey string, executeTime time.Time) error {
	redisKey := getRedisKey(bucketID, ruleID, prefix, operation)

	_, err := l.redis.ZAdd(redisKey, redis.Z{
		Score:  float64(executeTime.UnixMilli()),
		Member: objectKey,
	}).Result()
	if err != nil {
		return err
	}
	return nil
}

func (l *lifeCycle) GetPendingLifecycleEvents(ctx context.Context, bucketID int64, ruleID int64, prefix string, operation string) ([]string, error) {
	redisKey := getRedisKey(bucketID, ruleID, prefix, operation)
	strList, err := l.redis.ZRangeByScore(redisKey, redis.ZRangeBy{
		Min:    gconv.String(0),
		Max:    gconv.String(time.Now().UnixMilli()),
		Offset: 0,
		Count:  100, // 每次最多获取100个待处理事件
	}).Result()
	if err != nil {
		return nil, err
	}
	return strList, nil
}

func (l *lifeCycle) DelLifecycleEvent(ctx context.Context, bucketID int64, ruleID int64, prefix string, operation string, objectKey string) error {
	redisKey := getRedisKey(bucketID, ruleID, prefix, operation)
	_, err := l.redis.ZRem(redisKey, objectKey).Result()
	if err != nil {
		return err
	}
	return nil
}
