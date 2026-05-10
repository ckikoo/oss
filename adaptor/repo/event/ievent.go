package event

import (
	"context"
	"oss/service/do"
)

// IEventRuleRepo 事件规则仓库接口
type IEventRuleRepo interface {
	CreateEventRule(ctx context.Context, rule *do.EventRuleDo) (int64, error)
	GetByID(ctx context.Context, ruleID int64) (*do.EventRuleDo, error)
	GetByBucketIDAndRuleName(ctx context.Context, bucketID int64, ruleName string) (*do.EventRuleDo, error)
	ListByBucketID(ctx context.Context, bucketID int64) ([]*do.EventRuleDo, error)
	ListActiveRulesByBucketID(ctx context.Context, bucketID int64) ([]*do.EventRuleDo, error)
	UpdateEventRule(ctx context.Context, ruleID int64, update *do.UpdateEventRule) error
	DeleteEventRule(ctx context.Context, ruleID int64) error
}

// IEventDeliveryRepo 事件投递仓库接口
type IEventDeliveryRepo interface {
	CreateEventDelivery(ctx context.Context, delivery *do.EventDeliveryDo) (int64, error)
	GetPendingDeliveries(ctx context.Context, limit int) ([]*do.EventDeliveryDo, error)
	GetEventDeliveryByID(ctx context.Context, deliveryID int64) (*do.EventDeliveryDo, error)
	UpdateEventDelivery(ctx context.Context, deliveryID int64, update *do.UpdateEventDelivery) error
	DeleteEventDelivery(ctx context.Context, deliveryID int64) error
}
