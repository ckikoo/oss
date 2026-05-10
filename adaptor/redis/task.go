package redis

import (
	"context"
	"fmt"
	"oss/adaptor"
	"oss/consts"
	"time"

	"github.com/go-redis/redis/v8"
)

type ITask interface {
	// EnqueueTask 单个任务入队
	EnqueueTask(ctx context.Context, taskID string) error
	// EnqueueBatch 批量任务入队（一次 Pipeline，减少 RTT）
	EnqueueBatch(ctx context.Context, taskIDs []string) error
	// DequeueTask 阻塞等待至少 1 个任务，然后批量取出最多 size 个
	// timeout 超时后返回 nil, nil（无任务）
	DequeueTask(ctx context.Context, size int64, timeout time.Duration) ([]string, error)
	// QueueLen 查看队列当前积压长度（用于监控/告警）
	QueueLen(ctx context.Context) (int64, error)
}

type Task struct {
	rds *redis.Client
}

func taskQueueKey() string {
	return fmt.Sprintf("%s:task:queue", consts.ServerName)
}

func NewTask(adaptor adaptor.IAdaptor) *Task {
	return &Task{rds: adaptor.GetRedis()}
}

var _ ITask = (*Task)(nil)

// EnqueueTask 将单个 taskID 推入队列尾部（RPUSH）
// 配合 BLPOP 从头部取，实现 FIFO
func (t *Task) EnqueueTask(ctx context.Context, taskID string) error {
	return t.rds.RPush(ctx, taskQueueKey(), taskID).Err()
}

// EnqueueBatch 用 Pipeline 批量推入，减少网络往返
// 适用于 Recovery 扫描器一次重入多个卡住的任务
func (t *Task) EnqueueBatch(ctx context.Context, taskIDs []string) error {
	if len(taskIDs) == 0 {
		return nil
	}

	pipe := t.rds.Pipeline()
	for _, id := range taskIDs {
		pipe.RPush(ctx, taskQueueKey(), id)
	}
	_, err := pipe.Exec(ctx)
	return err
}

// DequeueTask 阻塞等待 + 批量取出
//
// 流程：
//  1. BLPOP 阻塞等待，直到有任务或 timeout 超时
//  2. 拿到第一个后，用 Lua 脚本原子地批量 Pop 剩余 (size-1) 个
//
// 返回 nil, nil 表示超时无任务，调用方循环重试即可
func (t *Task) DequeueTask(ctx context.Context, size int64, timeout time.Duration) ([]string, error) {
	if size <= 0 {
		return nil, nil
	}

	key := taskQueueKey()

	// Step 1: 阻塞等待第一个任务
	vals, err := t.rds.BLPop(ctx, timeout, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // 正常超时，无任务
		}
		return nil, err
	}
	first := vals[1] // BLPop 返回 [key, value]

	taskIDs := []string{first}

	// Step 2: 如果还需要更多，用 Lua 原子批量 Pop
	remaining := size - 1
	if remaining <= 0 {
		return taskIDs, nil
	}

	raw, err := luaBatchPop.Run(ctx, t.rds, []string{key}, remaining).Result()
	if err != nil && err != redis.Nil {
		// Lua 失败不影响已拿到的 first，降级返回
		return taskIDs, nil
	}

	if items, ok := raw.([]interface{}); ok {
		for _, item := range items {
			if s, ok := item.(string); ok {
				taskIDs = append(taskIDs, s)
			}
		}
	}

	return taskIDs, nil
}

// QueueLen 返回队列积压长度，用于 metrics 上报或告警
func (t *Task) QueueLen(ctx context.Context) (int64, error) {
	return t.rds.LLen(ctx, taskQueueKey()).Result()
}
