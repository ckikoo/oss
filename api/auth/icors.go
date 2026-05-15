package auth

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
)

type ICorsHandler interface {
	CreateBucketCorsRule(ctx context.Context, c *app.RequestContext)
	ListBucketCorsRules(ctx context.Context, c *app.RequestContext)
	UpdateBucketCorsRule(ctx context.Context, c *app.RequestContext)
	DeleteBucketCorsRule(ctx context.Context, c *app.RequestContext)
}
