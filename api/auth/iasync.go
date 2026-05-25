package auth

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
)

type IAsyncHandler interface {
	ListTasks(ctx context.Context, c *app.RequestContext)
	GetTask(ctx context.Context, c *app.RequestContext)
	RetryTask(ctx context.Context, c *app.RequestContext)
	CancelTask(ctx context.Context, c *app.RequestContext)
}
