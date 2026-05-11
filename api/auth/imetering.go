package auth

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
)

type IMeteringHandler interface {
	GetDailyMetering(ctx context.Context, c *app.RequestContext)
}
