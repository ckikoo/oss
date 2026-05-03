package do

import "time"

type BucketDo struct {
	ID           int64     `gorm:"column:id"`
	UserID       int64     `gorm:"column:user_id"`
	Name         string    `gorm:"column:name"`
	Region       string    `gorm:"column:region"`
	Acl          int32     `gorm:"column:acl"`
	Versioning   int32     `gorm:"column:versioning"`
	Status       int32     `gorm:"column:status"`
	StorageClass string    `gorm:"column:storage_class"`
	ObjectCount  int64     `gorm:"column:object_count"`
	StorageSize  int64     `gorm:"column:storage_size"`
	CreatedAt    time.Time `gorm:"column:created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at"`
}

type CreateBucket struct {
	UserID       int64
	Name         string
	Region       string
	Acl          int32
	Versioning   int32
	StorageClass string
}

type UpdateBucket struct {
	Region       string
	Acl          *int32
	Versioning   *int32
	Status       *int32
	StorageClass string
}
