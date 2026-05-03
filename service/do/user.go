package do

import "time"

type UserDo struct {
	ID           int64     `gorm:"column:id"`            // 用户ID
	Email        string    `gorm:"column:email"`         // 邮箱
	Status       int32     `gorm:"column:status"`        // 1=正常 2=禁用 3=注销
	StorageQuota int64     `gorm:"column:storage_quota"` // 存储配额(字节) 默认100GB
	StorageUsed  int64     `gorm:"column:storage_used"`  // 已用存储(字节)
	CreatedAt    time.Time `gorm:"column:created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at"`
}
type CreateUser struct {
	Email        string `json:"email"`
	StorageQuota int64  `json:"storage_quota"`
}
