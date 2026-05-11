package auth

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
)

type ILifecycleHandler interface {
	CreateLifecycleRule(ctx context.Context, c *app.RequestContext)
	ListLifecycleRules(ctx context.Context, c *app.RequestContext)
	GetLifecycleRule(ctx context.Context, c *app.RequestContext)
	UpdateLifecycleRule(ctx context.Context, c *app.RequestContext)
	DeleteLifecycleRule(ctx context.Context, c *app.RequestContext)
}
