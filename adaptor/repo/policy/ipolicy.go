package policy

import (
	"context"
	"oss/adaptor/tx"
	"oss/service/do"
)

type IPolicyRepo interface {
	WithTx(tx tx.Tx) IPolicyRepo
	CreateBucketPolicy(ctx context.Context, bucketID int64, policy *do.CreateBucketPolicy) (int64, error)
	ListBucketPolicies(ctx context.Context, bucketID int64) ([]*do.BucketPolicyDo, error)
}
