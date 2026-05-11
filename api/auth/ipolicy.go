package auth

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
)

type IPolicyHandler interface {
	CreateBucketPolicy(ctx context.Context, c *app.RequestContext)
	ListBucketPolicies(ctx context.Context, c *app.RequestContext)
}
