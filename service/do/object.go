package do

import (
	"time"

	"gorm.io/gorm"
)

type ObjectDo struct {
	ID            int64          `gorm:"column:id"`
	BucketID      int64          `gorm:"column:bucket_id"`
	BucketName    string         `gorm:"column:bucket_name"`
	ObjectKey     string         `gorm:"column:object_key"`
	ObjectKeyHash string         `gorm:"column:object_key_hash"`
	VersionID     string         `gorm:"column:version_id"`
	Size          int64          `gorm:"column:size"`
	Etag          string         `gorm:"column:etag"`
	ContentType   *string        `gorm:"column:content_type"`
	StorageClass  string         `gorm:"column:storage_class"`
	IsMultipart   int32          `gorm:"column:is_multipart"`
	UploadID      *string        `gorm:"column:upload_id"`
	StoragePath   *string        `gorm:"column:storage_path"`
	Acl           int32          `gorm:"column:acl"`
	Metadata      *string        `gorm:"column:metadata"`
	Status        int32          `gorm:"column:status"`
	AccessCount   int64          `gorm:"column:access_count"`
	CreatedAt     time.Time      `gorm:"column:created_at"`
	UpdatedAt     time.Time      `gorm:"column:updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"column:deleted_at"`
}

type CreateObject struct {
	BucketID      int64
	BucketName    string
	ObjectKey     string
	ObjectKeyHash string
	VersionID     string
	Size          int64
	Etag          string
	ContentType   *string
	StorageClass  string
	IsMultipart   int32
	UploadID      *string
	StoragePath   *string
	Acl           int32
	Metadata      *string
}

type UpdateObject struct {
	Size         *int64
	Etag         *string
	ContentType  *string
	StorageClass *string
	StoragePath  *string
	Acl          *int32
	Metadata     *string
	Status       *int32
}
