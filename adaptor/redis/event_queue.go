package redis

import (
	"context"
	"fmt"
	"oss/adaptor"
	"oss/consts"
	"strconv"
	"time"

	"github.com/go-redis/redis"
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

func (q *EventQueue) EnqueueDeliveryID(_ context.Context, deliveryID int64) error {
	return q.rds.RPush(eventDeliveryQueueKey(), strconv.FormatInt(deliveryID, 10)).Err()
}

func (q *EventQueue) EnqueueBatchDeliveryIDs(_ context.Context, deliveryIDs []int64) error {
	if len(deliveryIDs) == 0 {
		return nil
	}
	pipe := q.rds.Pipeline()
	for _, id := range deliveryIDs {
		pipe.RPush(eventDeliveryQueueKey(), strconv.FormatInt(id, 10))
	}
	_, err := pipe.Exec()
	return err
}

func (q *EventQueue) DequeueDeliveryIDs(ctx context.Context, size int64, timeout time.Duration) ([]int64, error) {
	if size <= 0 {
		return nil, nil
	}

	key := eventDeliveryQueueKey()

	type result struct {
		vals []string
		err  error
	}

	ch := make(chan result, 1)
	go func() {
		vals, err := q.rds.BLPop(timeout, key).Result()
		ch <- result{vals: vals, err: err}
	}()

	var first string
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		if res.err != nil {
			if res.err == redis.Nil {
				return nil, nil
			}
			return nil, res.err
		}
		if len(res.vals) < 2 {
			return nil, fmt.Errorf("unexpected BLPop result")
		}
		first = res.vals[1]
	}

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

	raw, err := luaBatchPop.Run(q.rds, []string{key}, remaining).Result()
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

func (q *EventQueue) QueueLen(_ context.Context) (int64, error) {
	return q.rds.LLen(eventDeliveryQueueKey()).Result()
}
