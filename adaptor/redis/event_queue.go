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

type IEventQueue interface {
	EnqueueDeliveryID(ctx context.Context, deliveryID int64) error
	EnqueueBatchDeliveryIDs(ctx context.Context, deliveryIDs []int64) error
	DequeueDeliveryIDs(ctx context.Context, size int64, timeout time.Duration) ([]int64, error)
	QueueLen(ctx context.Context) (int64, error)
}

type EventQueue struct {
	rds *redis.Client
}

func eventDeliveryQueueKey() string {
	return fmt.Sprintf("%s:event:delivery:queue", consts.ServerName)
}

func NewEventQueue(adaptor adaptor.IAdaptor) *EventQueue {
	return &EventQueue{rds: adaptor.GetRedis()}
}

var _ IEventQueue = (*EventQueue)(nil)

func (q *EventQueue) EnqueueDeliveryID(ctx context.Context, deliveryID int64) error {
	return q.rds.RPush(ctx, eventDeliveryQueueKey(), strconv.FormatInt(deliveryID, 10)).Err()
}

func (q *EventQueue) EnqueueBatchDeliveryIDs(ctx context.Context, deliveryIDs []int64) error {
	if len(deliveryIDs) == 0 {
		return nil
	}
	pipe := q.rds.Pipeline()
	for _, id := range deliveryIDs {
		pipe.RPush(ctx, eventDeliveryQueueKey(), strconv.FormatInt(id, 10))
	}
	_, err := pipe.Exec(ctx)
	return err
}

func (q *EventQueue) DequeueDeliveryIDs(ctx context.Context, size int64, timeout time.Duration) ([]int64, error) {
	if size <= 0 {
		return nil, nil
	}

	key := eventDeliveryQueueKey()

	vals, err := q.rds.BLPop(ctx, timeout, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}
	if len(vals) < 2 {
		return nil, fmt.Errorf("unexpected BLPop result")
	}
	first := vals[1]

	deliveryIDs := make([]int64, 0, size)
	parsed, err := strconv.ParseInt(first, 10, 64)
	if err != nil {
		return nil, err
	}
	deliveryIDs = append(deliveryIDs, parsed)

	remaining := size - 1
	if remaining <= 0 {
		return deliveryIDs, nil
	}

	raw, err := luaBatchPop.Run(ctx, q.rds, []string{key}, remaining).Result()
	if err != nil && err != redis.Nil {
		return deliveryIDs, nil
	}

	if items, ok := raw.([]interface{}); ok {
		for _, item := range items {
			if s, ok := item.(string); ok {
				if id, err := strconv.ParseInt(s, 10, 64); err == nil {
					deliveryIDs = append(deliveryIDs, id)
				} else {
					return deliveryIDs, err
				}
			}
		}
	}

	return deliveryIDs, nil
}

func (q *EventQueue) QueueLen(ctx context.Context) (int64, error) {
	return q.rds.LLen(ctx, eventDeliveryQueueKey()).Result()
}
