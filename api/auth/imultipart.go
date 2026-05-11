package auth

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
)

type IMultipartHandler interface {
	CreateMultipartUpload(ctx context.Context, c *app.RequestContext)
	UploadMultipartPart(ctx context.Context, c *app.RequestContext)
	CompleteMultipartUpload(ctx context.Context, c *app.RequestContext)
	AbortMultipartUpload(ctx context.Context, c *app.RequestContext)
}
