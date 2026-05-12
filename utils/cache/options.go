package cache

import "time"

func WithStreamName(name string) func(*ManagerConfig) {
	return func(c *ManagerConfig) {
		c.StreamName = name
	}
}

func WithLocalCapacity(cap int) func(*ManagerConfig) {
	return func(c *ManagerConfig) {
		c.LocalCapacity = cap
	}
}

func WithLocalTTL(ttl time.Duration) func(*ManagerConfig) {
	return func(c *ManagerConfig) {
		c.LocalTTL = ttl
	}
}
