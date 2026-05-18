package redis

import (
	"context"
	"fmt"
	"oss/adaptor"
	"oss/consts"
	"time"

	"github.com/go-redis/redis/v8"
)

// ILock 锁接口
type ILock interface {
	// 获取锁
	AcquireLock(ctx context.Context, key string, uuid string, ttl time.Duration) (bool, error)
	// 释放锁
	ReleaseLock(ctx context.Context, key string, uuid string) error

	// 刷新锁
	RefreshLock(ctx context.Context, key string, uuid string, ttl time.Duration) error
	// 检查锁状态
	CheckLock(ctx context.Context, key string, uuid string) (bool, error)
	// 检查锁是否存在
	LockExists(ctx context.Context, key string) (bool, error)

	// 强制释放锁（管理员操作）
	ForceReleaseLock(ctx context.Context, key string) error
}

type lock struct {
	redis *redis.Client
}

var _ ILock = (*lock)(nil)

// NewLock 创建锁实例
func NewLock(adaptor adaptor.IAdaptor) *lock {
	return &lock{
		redis: adaptor.GetRedis(),
	}
}

// generateLockKey 生成锁的 Redis key
func (fl *lock) generateLockKey(key string) string {
	return fmt.Sprintf("%s:lock:%s", consts.ServerName, key)
}
func (fl *lock) AcquireLock(ctx context.Context, key string, uuid string, ttl time.Duration) (bool, error) {
	lockKey := fl.generateLockKey(key)
	return fl.redis.SetNX(ctx, lockKey, uuid, ttl).Result()
}

// 释放锁
func (fl *lock) ReleaseLock(ctx context.Context, key string, uuid string) error {
	lockKey := fl.generateLockKey(key)
	result, err := luaUnlock.Run(ctx, fl.redis, []string{lockKey}, uuid).Result()
	if err != nil {
		return fmt.Errorf("failed to release lock: %w", err)
	}

	deletedCount, ok := result.(int64)
	if !ok || deletedCount == 0 {
		return fmt.Errorf("failed to release lock: lock not owned by this uuid")
	}

	return nil
}

// 刷新锁
func (fl *lock) RefreshLock(ctx context.Context, key string, uuid string, ttl time.Duration) error {
	lockKey := fl.generateLockKey(key)
	result, err := luaRefresh.Run(ctx, fl.redis, []string{lockKey}, uuid, ttl.Milliseconds()).Result()
	if err != nil {
		return fmt.Errorf("failed to refresh lock: %w", err)
	}

	refreshResult, ok := result.(int64)
	if !ok || refreshResult == 0 {
		return fmt.Errorf("failed to refresh lock: lock not owned by this uuid")
	}

	return nil
}

// 检查锁状态
func (fl *lock) CheckLock(ctx context.Context, key string, uuid string) (bool, error) {
	lockKey := fl.generateLockKey(key)
	val, err := fl.redis.Get(ctx, lockKey).Result()
	if err == redis.Nil {
		return false, nil // 锁不存在
	}
	if err != nil {
		return false, fmt.Errorf("failed to check lock: %w", err)
	}

	return val == uuid, nil // 返回是否由指定的 uuid 持有锁
}

// LockExists 检查指定 Redis 锁 key 是否存在
func (fl *lock) LockExists(ctx context.Context, key string) (bool, error) {
	lockKey := fl.generateLockKey(key)
	count, err := fl.redis.Exists(ctx, lockKey).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check lock exists: %w", err)
	}
	return count > 0, nil
}

// 强制释放锁（管理员操作）
func (fl *lock) ForceReleaseLock(ctx context.Context, key string) error {
	lockKey := fl.generateLockKey(key)
	err := fl.redis.Del(ctx, lockKey).Err()
	if err != nil {
		return fmt.Errorf("failed to force release lock: %w", err)
	}

	return nil
}
