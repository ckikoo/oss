package bucket

import (
	"context"
	"oss/service/do"
)

type IBucketRepo interface {
	CreateBucket(ctx context.Context, bucket *do.CreateBucket) (int64, error)
	GetByName(ctx context.Context, name string) (*do.BucketDo, error)
	GetByID(ctx context.Context, id int64) (*do.BucketDo, error)
	ListByFilter(ctx context.Context, userID int64, status int32) ([]*do.BucketDo, error)
	UpdateBucket(ctx context.Context, name string, update *do.UpdateBucket) (*do.BucketDo, error)
	DeleteBucket(ctx context.Context, name string) error

	UpdateBucketStats(ctx context.Context, bucketName string, deltaCount, deltaSize int64) error
}
