package auth

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
)

type IAccessKeyHandler interface {
	CreateAccessKey(ctx context.Context, c *app.RequestContext)
	ListAccessKeys(ctx context.Context, c *app.RequestContext)
	GetAccessKey(ctx context.Context, c *app.RequestContext)
	DeactivateAccessKey(ctx context.Context, c *app.RequestContext)
}
