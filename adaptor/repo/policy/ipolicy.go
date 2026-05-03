package policy

import (
	"context"
	"oss/service/do"
)

type IPolicyRepo interface {
	CreateBucketPolicy(ctx context.Context, bucketID int64, policy *do.CreateBucketPolicy) (int64, error)
	ListBucketPolicies(ctx context.Context, bucketID int64) ([]*do.BucketPolicyDo, error)
}
