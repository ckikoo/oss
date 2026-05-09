package object

import (
	"context"
	"oss/service/do"

	"gorm.io/gorm"
)

type IObjectRepo interface {
	CreateObject(ctx context.Context, object *do.CreateObject) (int64, error)
	GetObjectFromHashKey(ctx context.Context, object *do.GetObjectFromHashKey) (*do.ObjectDo, error)
	GetByKey(ctx context.Context, bucketName, objectKey, versionID string) (*do.ObjectDo, error)
	GetByKeyWithTx(tx *gorm.DB, ctx context.Context, bucketName, objectKey, versionID string) (*do.ObjectDo, error)
	ListByFilter(ctx context.Context, bucketName, prefix, delimiter, marker string, maxKeys int, versionID string) ([]*do.ObjectDo, error)
	UpdateObject(ctx context.Context, bucketName, objectKey, versionID string, update *do.UpdateObject) (*do.ObjectDo, error)
	UpdateObjectStorageClass(ctx context.Context, bucketName, objectKey, storageClass string) error
	DeleteObject(ctx context.Context, bucketName, objectKey, versionID string) error
	DeleteObjectWithTx(tx *gorm.DB, ctx context.Context, bucketName, objectKey, versionID string) error
}
