package auth

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
)

type IAuditHandler interface {
	ListOperationLogs(ctx context.Context, c *app.RequestContext)
}
