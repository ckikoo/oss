package cors

import (
	"context"

	"oss/adaptor/tx"
	"oss/service/do"
)

type IBucketCorsRepo interface {
	WithTx(tx tx.Tx) IBucketCorsRepo
	Create(ctx context.Context, rule *do.CreateBucketCorsRule) (*do.BucketCorsRuleDo, error)
	ListByBucket(ctx context.Context, userID int64, bucketName string) ([]*do.BucketCorsRuleDo, error)
	GetMatchedRule(ctx context.Context, userID int64, bucketName, origin string) (*do.BucketCorsRuleDo, error)
	GetByID(ctx context.Context, userID int64, bucketName string, ruleID int64) (*do.BucketCorsRuleDo, error)
	Update(ctx context.Context, userID int64, bucketName string, ruleID int64, update *do.UpdateBucketCorsRule) (*do.BucketCorsRuleDo, error)
	Delete(ctx context.Context, userID int64, bucketName string, ruleID int64) error
}
