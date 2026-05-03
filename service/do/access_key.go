package do

import "time"

type AccessKeyDo struct {
	ID         int64
	UserID     int64
	AccessKey  string
	SecretKey  string
	Alias      string
	Status     int32
	Permission string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastUsedAt time.Time
}

type CreateAccessKey struct {
	UserID     int64 `json:"user_id"`
	AccessKey  string
	SecretKey  string
	Permission string     `json:"permission,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}
