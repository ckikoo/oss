package object

import (
	"context"
	"oss/adaptor/tx"
	"oss/service/do"
)

// versionID 一定要传递，除了getbykey可以传空版本ID之外，其他接口都必须传递版本ID
type IObjectRepo interface {
	WithTx(tx tx.Tx) IObjectRepo
	CreateObject(ctx context.Context, object *do.CreateObject) (int64, error)
	GetByKey(ctx context.Context, bucketName, objectKey, versionID string) (*do.ObjectDo, error)
	ListByFilter(ctx context.Context, bucketName, prefix, delimiter, marker string, maxKeys int, versionID string) ([]*do.ObjectDo, error)
	ListVersionsByFilter(ctx context.Context, bucketName, objectKey string) ([]*do.ObjectDo, error)
	UpdateObject(ctx context.Context, bucketName, objectKey, versionID string, update *do.UpdateObject) (*do.ObjectDo, error)
	UpdateObjectStorageClass(ctx context.Context, bucketName, objectKey, storageClass string) error
	DeleteObject(ctx context.Context, bucketName, objectKey string, versionID ...string) error
	GetLastVersion(ctx context.Context, bucketName, objectKey string) (string, error) // 只要versionID，不需要其他字段
	ListByBucketWithPrefix(ctx context.Context, list *do.ListObjectsByBucket) ([]*do.ObjectDo, error)
	UpdateObjectNotLatest(ctx context.Context, bucketName, objectKey string, version string) error
}
