package object

import (
	"context"
	"oss/service/do"
)

type IObjectRepo interface {
	CreateObject(ctx context.Context, object *do.CreateObject) (int64, error)
	GetByKey(ctx context.Context, bucketName, objectKey, versionID string) (*do.ObjectDo, error)
	ListByFilter(ctx context.Context, bucketName, prefix, delimiter, marker string, maxKeys int, versionID string) ([]*do.ObjectDo, error)
	UpdateObject(ctx context.Context, bucketName, objectKey, versionID string, update *do.UpdateObject) (*do.ObjectDo, error)
	DeleteObject(ctx context.Context, bucketName, objectKey, versionID string) error
}
