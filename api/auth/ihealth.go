package auth

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
)

type IHealthHandler interface {
	Healthz(ctx context.Context, c *app.RequestContext)
	Readyz(ctx context.Context, c *app.RequestContext)
}
