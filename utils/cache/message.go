package cache

import "time"

type InvalidationMsg struct {
	Keys      []string  `json:"keys"`
	SenderID  string    `json:"sender_id"`
	PublishAt time.Time `json:"publish_at"`
}
