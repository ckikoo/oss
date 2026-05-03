package auth

import (
	"context"
	"oss/api"
	"oss/common"
	"oss/service/bucket"
	"oss/service/dto"

	"github.com/cloudwego/hertz/pkg/app"
)

type BucketCtrl struct {
	bucket *bucket.Service
}

func NewBucketCtrl(service *bucket.Service) *BucketCtrl {
	return &BucketCtrl{bucket: service}
}

func (ctrl *BucketCtrl) CreateBucket(ctx context.Context, c *app.RequestContext) {
	req := &dto.CreateBucketReq{}
	if err := c.BindAndValidate(req); err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
		return
	}

	resp, errno := ctrl.bucket.CreateBucket(ctx, req)
	api.WriteResp(c, resp, errno)
}

func (ctrl *BucketCtrl) ListBuckets(ctx context.Context, c *app.RequestContext) {
	req := &dto.ListBucketsReq{}
	if err := c.BindAndValidate(req); err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
		return
	}

	resp, errno := ctrl.bucket.ListBuckets(ctx, req)
	api.WriteResp(c, resp, errno)
}

func (ctrl *BucketCtrl) GetBucket(ctx context.Context, c *app.RequestContext) {
	bucketName := c.Param("bucket_name")
	if bucketName == "" {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("bucket_name is required"))
		return
	}

	resp, errno := ctrl.bucket.GetBucket(ctx, bucketName)
	api.WriteResp(c, resp, errno)
}

func (ctrl *BucketCtrl) UpdateBucket(ctx context.Context, c *app.RequestContext) {
	bucketName := c.Param("bucket_name")
	if bucketName == "" {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("bucket_name is required"))
		return
	}

	req := &dto.UpdateBucketReq{}
	if err := c.BindAndValidate(req); err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
		return
	}

	resp, errno := ctrl.bucket.UpdateBucket(ctx, bucketName, req)
	api.WriteResp(c, resp, errno)
}

func (ctrl *BucketCtrl) DeleteBucket(ctx context.Context, c *app.RequestContext) {
	bucketName := c.Param("bucket_name")
	if bucketName == "" {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("bucket_name is required"))
		return
	}

	errno := ctrl.bucket.DeleteBucket(ctx, bucketName)
	api.WriteResp(c, nil, errno)
}
