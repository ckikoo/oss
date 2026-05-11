package auth

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
)

type IObjectHandler interface {
	ListObjects(ctx context.Context, c *app.RequestContext)
	GetObjectMetadata(ctx context.Context, c *app.RequestContext)
	GetObjectVersions(ctx context.Context, c *app.RequestContext)
	PutObject(ctx context.Context, c *app.RequestContext)
	GetObject(ctx context.Context, c *app.RequestContext)
	DeleteObject(ctx context.Context, c *app.RequestContext)
}
