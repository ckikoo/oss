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

	return t.rds.RPush(ctx, taskQueueKey(), strconv.FormatInt(taskID, 10)).Err()
}

func (t *Task) EnqueueBatch(ctx context.Context, taskIDs []int64) error {
	if len(taskIDs) == 0 {
		return nil
	}

	pipe := t.rds.Pipeline()
	hasCommand := false
	for _, taskID := range taskIDs {
		if taskID <= 0 {
			continue
		}
		pipe.RPush(ctx, taskQueueKey(), strconv.FormatInt(taskID, 10))
		hasCommand = true
	}
	if !hasCommand {
		return nil
	}

	_, err := pipe.Exec(ctx)
	return err
}

func (t *Task) DequeueTask(ctx context.Context, size int64, timeout time.Duration) ([]int64, error) {
	if size <= 0 {
		return nil, nil
	}

	vals, err := t.rds.BLPop(ctx, timeout, taskQueueKey()).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(vals) != 2 {
		return nil, fmt.Errorf("unexpected BLPop result")
	}

	taskIDs := make([]int64, 0, size)
	appendTaskID := func(raw string) {
		taskID, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr == nil && taskID > 0 {
			taskIDs = append(taskIDs, taskID)
		}
	}
	appendTaskID(vals[1])

	for int64(len(taskIDs)) < size {
		raw, popErr := t.rds.LPop(ctx, taskQueueKey()).Result()
		if popErr == redis.Nil {
			break
		}
		if popErr != nil {
			return taskIDs, popErr
		}
		appendTaskID(raw)
	}

	return taskIDs, nil
}

func (t *Task) QueueLen(ctx context.Context) (int64, error) {
	return t.rds.LLen(ctx, taskQueueKey()).Result()
}
