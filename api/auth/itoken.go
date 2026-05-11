package auth

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
)

type ITokenHandler interface {
	CreateUploadToken(ctx context.Context, c *app.RequestContext)
	CreateDownloadToken(ctx context.Context, c *app.RequestContext)

	ValidateToken(ctx context.Context, token, action, expectedBucketName, expectedObjectKey string) (ak string, pass bool)
}
