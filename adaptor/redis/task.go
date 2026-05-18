package redis

import (
	"context"
	"fmt"
	"oss/adaptor"
	"oss/consts"
	"strconv"
	"time"

	"github.com/go-redis/redis/v8"
)

type ITask interface {
	EnqueueTask(ctx context.Context, taskID int64) error
	EnqueueBatch(ctx context.Context, taskIDs []int64) error
	DequeueTask(ctx context.Context, size int64, timeout time.Duration) ([]int64, error)
	QueueLen(ctx context.Context) (int64, error)
}

type Task struct {
	rds *redis.Client
}

func taskQueueKey() string {
	return fmt.Sprintf("%s:task:ready", consts.ServerName)
}

func NewTask(adaptor adaptor.IAdaptor) *Task {
	return &Task{rds: adaptor.GetRedis()}
}

var _ ITask = (*Task)(nil)

func (t *Task) EnqueueTask(ctx context.Context, taskID int64) error {
	if taskID <= 0 {
		return nil
	}

	return t.rds.ZAdd(ctx, taskQueueKey(), &redis.Z{
		Score:  float64(taskID),
		Member: taskID,
	}).Err()
}

func (t *Task) EnqueueBatch(ctx context.Context, taskIDs []int64) error {
	if len(taskIDs) == 0 {
		return nil
	}

	members := make([]*redis.Z, 0, len(taskIDs))
	for _, taskID := range taskIDs {
		if taskID <= 0 {
			continue
		}
		members = append(members, &redis.Z{
			Score:  float64(taskID),
			Member: taskID,
		})
	}
	if len(members) == 0 {
		return nil
	}

	return t.rds.ZAddNX(ctx, taskQueueKey(), members...).Err()
}

func (t *Task) DequeueTask(ctx context.Context, size int64, timeout time.Duration) ([]int64, error) {
	if size <= 0 {
		return nil, nil
	}

	deadline := time.Now().Add(timeout)
	for {
		taskIDs, err := t.popReady(ctx, size)
		if err != nil || len(taskIDs) > 0 || timeout <= 0 {
			return taskIDs, err
		}

		wait := time.Until(deadline)
		if wait <= 0 {
			return nil, nil
		}
		if wait > 200*time.Millisecond {
			wait = 200 * time.Millisecond
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (t *Task) popReady(ctx context.Context, size int64) ([]int64, error) {
	raw, err := luaZPopReady.Run(ctx, t.rds, []string{taskQueueKey()}, size).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}

	if items, ok := raw.([]string); ok {
		return parseTaskIDs(items), nil
	}

	items, ok := raw.([]interface{})
	if !ok {
		return nil, nil
	}

	taskIDs := make([]int64, 0, len(items))
	for _, item := range items {
		taskID, err := strconv.ParseInt(fmt.Sprint(item), 10, 64)
		if err == nil && taskID > 0 {
			taskIDs = append(taskIDs, taskID)
		}
	}

	return taskIDs, nil
}

func parseTaskIDs(items []string) []int64 {
	taskIDs := make([]int64, 0, len(items))
	for _, item := range items {
		taskID, err := strconv.ParseInt(item, 10, 64)
		if err == nil && taskID > 0 {
			taskIDs = append(taskIDs, taskID)
		}
	}
	return taskIDs
}

func (t *Task) QueueLen(ctx context.Context) (int64, error) {
	return t.rds.ZCard(ctx, taskQueueKey()).Result()
}
