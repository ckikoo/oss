package router

import (
	"context"
	"net/http"
	"oss/adaptor"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/route"
)

func registerS3Routes(h *server.Hertz, deps RouterDeps, adaptor adaptor.IAdaptor) {
	s3Group := h.Group("/", NewS3SignatureV4Middleware(adaptor))

	s3Group.GET("", deps.S3Handler.ListBuckets)
	s3Group.PUT(":bucket_name", deps.S3Handler.CreateBucket)
	s3Group.HEAD(":bucket_name", deps.S3Handler.HeadBucket)
	s3Group.POST(":bucket_name", dispatchS3BucketPost(deps))
	s3Group.DELETE(":bucket_name", deps.S3Handler.DeleteBucket)

	registerS3ObjectRoutes(s3Group, deps)
}

func registerS3ObjectRoutes(s3Group *route.RouterGroup, deps RouterDeps) {
	s3Group.GET(":bucket_name", dispatchS3BucketGet(deps))
	s3Group.POST(":bucket_name/*object_key", dispatchS3ObjectPost(deps))
	s3Group.PUT(":bucket_name/*object_key", dispatchS3ObjectPut(deps))
	s3Group.GET(":bucket_name/*object_key", dispatchS3ObjectGet(deps))
	s3Group.HEAD(":bucket_name/*object_key", deps.S3Handler.HeadObject)
	s3Group.DELETE(":bucket_name/*object_key", dispatchS3ObjectDelete(deps))
}

func dispatchS3BucketGet(deps RouterDeps) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if hasS3Subresource(c, "location") {
			deps.S3Handler.GetBucketLocation(ctx, c)
			return
		}
		if c.Query("list-type") == "2" {
			deps.S3Handler.ListObjectsV2(ctx, c)
			return
		}
		deps.S3Handler.ListObjectsV2(ctx, c)
	}
}

func dispatchS3BucketPost(deps RouterDeps) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if hasS3Subresource(c, "delete") {
			deps.S3Handler.DeleteObjects(ctx, c)
			return
		}
		c.AbortWithStatus(http.StatusNotFound)
	}
}

func dispatchS3ObjectPut(deps RouterDeps) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if c.Query("partNumber") != "" && c.Query("uploadId") != "" {
			deps.S3Handler.UploadPart(ctx, c)
			return
		}
		if string(c.GetHeader("x-amz-copy-source")) != "" {
			deps.S3Handler.CopyObject(ctx, c)
			return
		}
		deps.S3Handler.PutObject(ctx, c)
	}
}

func dispatchS3ObjectGet(deps RouterDeps) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if c.Query("uploadId") != "" {
			deps.S3Handler.ListParts(ctx, c)
			return
		}
		deps.S3Handler.GetObject(ctx, c)
	}
}

func dispatchS3ObjectPost(deps RouterDeps) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if hasS3Subresource(c, "uploads") {
			deps.S3Handler.CreateMultipartUpload(ctx, c)
			return
		}
		if c.Query("uploadId") != "" {
			deps.S3Handler.CompleteMultipartUpload(ctx, c)
			return
		}
		c.AbortWithStatus(http.StatusNotFound)
	}
}

func dispatchS3ObjectDelete(deps RouterDeps) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if c.Query("uploadId") != "" {
			deps.S3Handler.AbortMultipartUpload(ctx, c)
			return
		}
		deps.S3Handler.DeleteObject(ctx, c)
	}
}

func hasS3Subresource(c *app.RequestContext, name string) bool {
	_, ok := c.GetQuery(name)
	return ok
}
