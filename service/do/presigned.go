package do

import "time"

type CreatePresignedURL struct {
	Token         string
	BucketID      int64
	ObjectKey     string
	ObjectKeyHash string
	Method        string
	SingleUse     int32
	UserID        int64
	ExpiresAt     time.Time
}

type PresignedURLDo struct {
	ID            int64
	Token         string
	BucketID      int64
	ObjectKey     string
	ObjectKeyHash string
	Method        string
	SingleUse     int32
	Used          int32
	UserID        int64
	ExpiresAt     time.Time
	CreatedAt     time.Time
}
