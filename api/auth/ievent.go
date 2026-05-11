package auth

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
)

type IEventHandler interface {
	CreateEventRule(ctx context.Context, c *app.RequestContext)
	ListEventRules(ctx context.Context, c *app.RequestContext)
	UpdateEventRule(ctx context.Context, c *app.RequestContext)
	DeleteEventRule(ctx context.Context, c *app.RequestContext)
}
