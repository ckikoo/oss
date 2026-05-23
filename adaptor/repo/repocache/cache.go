package repocache

import (
	"context"
	"reflect"
	"time"

	"golang.org/x/sync/singleflight"

	"oss/adaptor/tx"
	"oss/utils/cache"
	jsonutil "oss/utils/json"
	"oss/utils/logger"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
)

type Loader[T any] func(context.Context) (T, error)

type Accessor[T any] struct {
	RDS         *redis.Client
	Local       cache.IManager
	Group       *singleflight.Group
	TTL         time.Duration
	Enabled     bool
	ShouldCache func(T) bool
	LogName     string
}

func (a Accessor[T]) Get(ctx context.Context, key string, load Loader[T]) (T, error) {
	var zero T
	if load == nil {
		return zero, nil
	}
	if !a.Enabled || key == "" || a.RDS == nil || a.Local == nil || a.Group == nil {
		return load(ctx)
	}

	if value, ok := a.getLocal(key); ok {
		return value, nil
	}
	if value, ok := a.getRedis(ctx, key); ok {
		a.setLocal(key, value)
		return value, nil
	}

	result, err, _ := a.Group.Do(key, func() (interface{}, error) {
		if value, ok := a.getRedis(ctx, key); ok {
			return value, nil
		}
		value, err := load(ctx)
		if err != nil {
			return zero, err
		}
		if ctx.Err() == nil && a.shouldCache(value) {
			a.setAll(ctx, key, value)
		}
		return value, nil
	})
	if err != nil {
		return zero, err
	}

	value, ok := result.(T)
	if !ok {
		a.Local.Remove(key)
		return zero, nil
	}
	if a.shouldCache(value) {
		a.setLocal(key, value)
	}
	return value, nil
}

func (a Accessor[T]) getLocal(key string) (T, bool) {
	var zero T
	entry, ok := a.Local.Get(key)
	if !ok {
		return zero, false
	}
	value, ok := entry.Data.(T)
	if !ok {
		a.Local.Remove(key)
		return zero, false
	}
	return value, true
}

func (a Accessor[T]) getRedis(ctx context.Context, key string) (T, bool) {
	var zero T
	raw, err := a.RDS.Get(ctx, key).Result()
	if err != nil {
		if err != redis.Nil {
			a.warn("failed to get redis cache", zap.Error(err), zap.String("key", key))
		}
		return zero, false
	}
	value, err := a.unmarshal(raw)
	if err != nil {
		a.warn("failed to unmarshal redis cache", zap.Error(err), zap.String("key", key))
		return zero, false
	}
	if !a.shouldCache(value) {
		return zero, false
	}
	return value, true
}

func (a Accessor[T]) setAll(ctx context.Context, key string, value T) {
	a.setLocal(key, value)
	data, err := a.marshal(value)
	if err != nil {
		a.warn("failed to marshal cache value", zap.Error(err), zap.String("key", key))
		return
	}
	ttl := a.TTL
	if ttl <= 0 {
		ttl = time.Minute
	}
	if err := a.RDS.Set(ctx, key, data, ttl).Err(); err != nil {
		a.warn("failed to set redis cache", zap.Error(err), zap.String("key", key))
	}
}

func (a Accessor[T]) setLocal(key string, value T) {
	if !a.shouldCache(value) {
		return
	}
	a.Local.Set(key, value, 0)
}

func (a Accessor[T]) marshal(value T) (string, error) {
	return jsonutil.MarshalString(value)
}

func (a Accessor[T]) unmarshal(raw string) (T, error) {
	var value T
	err := jsonutil.UnmarshalString(raw, &value)
	return value, err
}

func (a Accessor[T]) shouldCache(value T) bool {
	if a.ShouldCache != nil {
		return a.ShouldCache(value)
	}
	return !isNil(value)
}

func (a Accessor[T]) warn(msg string, fields ...zap.Field) {
	if a.LogName != "" {
		fields = append(fields, zap.String("cache", a.LogName))
	}
	logger.GetLogger().Warn(msg, fields...)
}

type Invalidator struct {
	RDS          *redis.Client
	Local        cache.IManager
	BatchSize    int
	DoubleDelete bool
	Delay        time.Duration
	Timeout      time.Duration
	LogName      string
}

func (i Invalidator) AfterCommit(ctx context.Context, keys ...string) {
	keys = unique(keys)
	if len(keys) == 0 || (i.RDS == nil && i.Local == nil) {
		return
	}
	tx.AfterCommitOrNow(ctx, func(runCtx context.Context) {
		i.Invalidate(runCtx, keys...)
	})
}

func (i Invalidator) Invalidate(ctx context.Context, keys ...string) {
	keys = unique(keys)
	if len(keys) == 0 || (i.RDS == nil && i.Local == nil) {
		return
	}
	i.invalidateOnce(ctx, keys...)
	if !i.DoubleDelete {
		return
	}
	delay := i.Delay
	if delay <= 0 {
		delay = 200 * time.Millisecond
	}
	timeout := i.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	time.AfterFunc(delay, func() {
		delCtx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		i.invalidateOnce(delCtx, keys...)
	})
}

func (i Invalidator) invalidateOnce(ctx context.Context, keys ...string) {
	batchSize := i.BatchSize
	if batchSize <= 0 {
		batchSize = 500
	}
	for start := 0; start < len(keys); start += batchSize {
		end := start + batchSize
		if end > len(keys) {
			end = len(keys)
		}
		batch := keys[start:end]
		if i.RDS != nil {
			if err := i.RDS.Del(ctx, batch...).Err(); err != nil {
				i.warn("failed to delete redis cache", zap.Error(err), zap.Strings("keys", batch))
			}
		}
		if i.Local == nil {
			continue
		}
		i.Local.Remove(batch...)
		if err := i.Local.Publish(ctx, batch...); err != nil {
			i.warn("failed to publish cache invalidation", zap.Error(err), zap.Strings("keys", batch))
		}
	}
}

func (i Invalidator) warn(msg string, fields ...zap.Field) {
	if i.LogName != "" {
		fields = append(fields, zap.String("cache", i.LogName))
	}
	logger.GetLogger().Warn(msg, fields...)
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

func unique(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(keys))
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	return result
}
