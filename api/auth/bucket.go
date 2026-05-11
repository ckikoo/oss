package auth

import (
	"context"
	"oss/adaptor"
	"oss/api"
	"oss/common"
	"oss/consts"
	"oss/service/bucket"
	"oss/service/dto"

	"github.com/cloudwego/hertz/pkg/app"
)

type BucketCtrl struct {
	bucket *bucket.Service
}

var _ IBucketHandler = (*BucketCtrl)(nil)

func NewBucketCtrl(adaptor adaptor.IAdaptor) IBucketHandler {
	return &BucketCtrl{bucket: bucket.NewService(adaptor)}
}

func (ctrl *BucketCtrl) CreateBucket(ctx context.Context, c *app.RequestContext) {
	req := &dto.CreateBucketReq{}
	if err := c.BindAndValidate(req); err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
		return
	}

	ctx1, pass := common.GetUserInfoFromContext(ctx, c)
	if !pass {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

	req.UserID = ctx1.UserID
	resp, errno := ctrl.bucket.CreateBucket(ctx1, req)
	api.WriteResp(c, resp, errno)
}

func (ctrl *BucketCtrl) ListBuckets(ctx context.Context, c *app.RequestContext) {

	req := &dto.ListBucketsReq{}

	if err := c.BindAndValidate(req); err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
		return
	}

	ctx1, pass := common.GetUserInfoFromContext(ctx, c)
	if !pass {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

	resp, errno := ctrl.bucket.ListBuckets(ctx1, req)
	api.WriteResp(c, resp, errno)
}

func (ctrl *BucketCtrl) GetBucket(ctx context.Context, c *app.RequestContext) {
	bucketName := c.Param("bucket_name")
	if bucketName == "" {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("bucket_name is required"))
		return
	}
	ctx1, pass := common.GetUserInfoFromContext(ctx, c)
	if !pass {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

	resp, errno := ctrl.bucket.GetBucket(ctx1, bucketName)
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

	ctx1, pass := common.GetUserInfoFromContext(ctx, c)
	if !pass {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

	resp, errno := ctrl.bucket.UpdateBucket(ctx1, bucketName, req)
	api.WriteResp(c, resp, errno)
}

func (ctrl *BucketCtrl) DeleteBucket(ctx context.Context, c *app.RequestContext) {
	bucketName := c.Param("bucket_name")
	if bucketName == "" {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("bucket_name is required"))
		return
	}

	ctx1, pass := common.GetUserInfoFromContext(ctx, c)
	if !pass {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

	bucket, ok := c.Get(consts.BucketContext)
	if !ok {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("bucket not found"))
		return
	}

	bucketDo, ok := bucket.(*dto.BucketItem)
	if !ok {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("invalid bucket info"))
		return
	}

	errno := ctrl.bucket.DeleteBucket(ctx1, bucketDo.ID, bucketName)
	api.WriteResp(c, nil, errno)

}
