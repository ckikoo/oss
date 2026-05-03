package redis

import (
	"context"
	"fmt"
	"oss/adaptor"
	"oss/config"
	"time"

	"github.com/go-redis/redis"
)

// IFileLock 文件锁接口
type IFileLock interface {
	// 获取锁
	AcquireLock(ctx context.Context, bucketName string, objectName string, uuid string, ttl time.Duration) (bool, error)
	// 释放锁
	ReleaseLock(ctx context.Context, bucketName string, objectName string, uuid string) error

	// 刷新锁
	RefreshLock(ctx context.Context, bucketName string, objectName string, uuid string, ttl time.Duration) (bool, error)
	// 检查锁状态
	CheckLock(ctx context.Context, bucketName string, objectName string, uuid string) (bool, error)

	// 强制释放锁（管理员操作）
	ForceReleaseLock(ctx context.Context, bucketName string, objectName string) error
}

type fileLock struct {
	redis *redis.Client
}

var _ IFileLock = (*fileLock)(nil)

// NewFileLock 创建文件锁实例
func NewFileLock(adaptor adaptor.IAdaptor) *fileLock {
	return &fileLock{
		redis: adaptor.GetRedis(),
	}
}

// generateLockKey 生成锁的 Redis key
func (fl *fileLock) generateLockKey(bucketName string, objectName string) string {
	return fmt.Sprintf("%s:lock:file:%s:%s", config.ServerName, bucketName, objectName)
}
func (fl *fileLock) AcquireLock(ctx context.Context, bucketName string, objectName string, uuid string, ttl time.Duration) (bool, error) {
	lockKey := fl.generateLockKey(bucketName, objectName)
	return fl.redis.SetNX(lockKey, uuid, ttl).Result()
}

// 释放锁
func (fl *fileLock) ReleaseLock(ctx context.Context, bucketName string, objectName string, uuid string) error {
	lockKey := fl.generateLockKey(bucketName, objectName)
	result, err := luaUnlock.Run(fl.redis, []string{lockKey}, uuid).Result()
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
func (fl *fileLock) RefreshLock(ctx context.Context, bucketName string, objectName string, uuid string, ttl time.Duration) (bool, error) {
	lockKey := fl.generateLockKey(bucketName, objectName)
	result, err := luaRefresh.Run(fl.redis, []string{lockKey}, uuid, ttl.Seconds()).Result()
	if err != nil {
		return false, fmt.Errorf("failed to refresh lock: %w", err)
	}

	refreshResult, ok := result.(int64)
	if !ok || refreshResult == 0 {
		return false, fmt.Errorf("failed to refresh lock: lock not owned by this uuid")
	}

	return true, nil
}

// 检查锁状态
func (fl *fileLock) CheckLock(ctx context.Context, bucketName string, objectName string, uuid string) (bool, error) {
	lockKey := fl.generateLockKey(bucketName, objectName)
	val, err := fl.redis.Get(lockKey).Result()
	if err == redis.Nil {
		return false, nil // 锁不存在
	}
	if err != nil {
		return false, fmt.Errorf("failed to check lock: %w", err)
	}

	return val == uuid, nil // 返回是否由指定的 uuid 持有锁
}

// 强制释放锁（管理员操作）
func (fl *fileLock) ForceReleaseLock(ctx context.Context, bucketName string, objectName string) error {
	lockKey := fl.generateLockKey(bucketName, objectName)
	err := fl.redis.Del(lockKey).Err()
	if err != nil {
		return fmt.Errorf("failed to force release lock: %w", err)
	}

	return nil
}
