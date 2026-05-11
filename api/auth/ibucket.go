package auth

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
)

type IBucketHandler interface {
	CreateBucket(ctx context.Context, c *app.RequestContext)
	ListBuckets(ctx context.Context, c *app.RequestContext)
	GetBucket(ctx context.Context, c *app.RequestContext)
	UpdateBucket(ctx context.Context, c *app.RequestContext)
	DeleteBucket(ctx context.Context, c *app.RequestContext)
}
