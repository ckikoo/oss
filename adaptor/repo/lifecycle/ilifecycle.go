package lifecycle

import (
	"context"
	"oss/adaptor/tx"
	"oss/service/do"
)

type ILifecycleRepo interface {
	WithTx(tx tx.Tx) ILifecycleRepo
	CreateLifecycleRule(ctx context.Context, rule *do.CreateLifecycleRule) (int64, error)
	ListLifecycleRules(ctx context.Context, bucketID int64) ([]*do.LifecycleRuleDo, error)
	ListAllActiveLifecycleRules(ctx context.Context) ([]*do.LifecycleRuleDo, error)
	GetLifecycleRule(ctx context.Context, bucketID, ruleID int64) (*do.LifecycleRuleDo, error)
	UpdateLifecycleRule(ctx context.Context, bucketID, ruleID int64, update *do.UpdateLifecycleRule) (*do.LifecycleRuleDo, error)
	DeleteLifecycleRule(ctx context.Context, bucketID, ruleID int64) error
}
