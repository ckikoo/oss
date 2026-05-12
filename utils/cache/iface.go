package cache

import (
	"context"
	"time"
)

type ILocalCache interface {
	Get(key string) (*Entry, bool)
	Set(key string, value any, ttl time.Duration)
	Remove(keys ...string)
}

type IPublisher interface {
	Publish(ctx context.Context, keys ...string) error
}

type ISubscriber interface {
	Start(ctx context.Context) error
	Stop()
}

// IManager repo 侧依赖这个
type IManager interface {
	ILocalCache
	IPublisher
}
