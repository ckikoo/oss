package s3

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
)

type IS3Handler interface {
	ListBuckets(ctx context.Context, c *app.RequestContext)
	CreateBucket(ctx context.Context, c *app.RequestContext)
	HeadBucket(ctx context.Context, c *app.RequestContext)
	GetBucketLocation(ctx context.Context, c *app.RequestContext)
	DeleteBucket(ctx context.Context, c *app.RequestContext)

	ListObjectsV2(ctx context.Context, c *app.RequestContext)
	PutObject(ctx context.Context, c *app.RequestContext)
	GetObject(ctx context.Context, c *app.RequestContext)
	HeadObject(ctx context.Context, c *app.RequestContext)
	DeleteObject(ctx context.Context, c *app.RequestContext)
	DeleteObjects(ctx context.Context, c *app.RequestContext)
	CopyObject(ctx context.Context, c *app.RequestContext)

	CreateMultipartUpload(ctx context.Context, c *app.RequestContext)
	UploadPart(ctx context.Context, c *app.RequestContext)
	ListParts(ctx context.Context, c *app.RequestContext)
	CompleteMultipartUpload(ctx context.Context, c *app.RequestContext)
	AbortMultipartUpload(ctx context.Context, c *app.RequestContext)
}
