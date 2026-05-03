package dto

import "time"

type User struct {
	ID           int64     `json:"id"`
	Email        string    `json:"email"`
	Status       int32     `json:"status"`
	StorageQuota int64     `json:"storage_quota"`
	StorageUsed  int64     `json:"storage_used"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type CreateUserReq struct {
	Email        string `json:"email"`
	StorageQuota int64  `json:"storage_quota"`
}
