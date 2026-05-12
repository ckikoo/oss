package cache

import "time"

type Entry struct {
	Data      any
	ExpiresAt time.Time
}

func NewEntry(data any, ttl time.Duration) *Entry {
	return &Entry{
		Data:      data,
		ExpiresAt: time.Now().Add(ttl),
	}
}

func (e *Entry) Expired() bool {
	return time.Now().After(e.ExpiresAt)
}
