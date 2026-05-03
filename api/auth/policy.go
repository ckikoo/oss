package auth

import (
	"context"
	"oss/api"
	"oss/common"
	"oss/service/dto"
	"oss/service/policy"

	"github.com/cloudwego/hertz/pkg/app"
)

type PolicyCtrl struct {
	policy *policy.Service
}

func NewPolicyCtrl(service *policy.Service) *PolicyCtrl {
	return &PolicyCtrl{policy: service}
}

func (ctrl *PolicyCtrl) CreateBucketPolicy(ctx context.Context, c *app.RequestContext) {
	bucketName := c.Param("bucket_name")
	if bucketName == "" {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("bucket_name is required"))
		return
	}

	req := &dto.CreateBucketPolicyReq{}
	if err := c.BindAndValidate(req); err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
		return
	}

	resp, errno := ctrl.policy.CreateBucketPolicy(ctx, bucketName, req)
	api.WriteResp(c, resp, errno)
}

func (ctrl *PolicyCtrl) ListBucketPolicies(ctx context.Context, c *app.RequestContext) {
	bucketName := c.Param("bucket_name")
	if bucketName == "" {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("bucket_name is required"))
		return
	}

	resp, errno := ctrl.policy.ListBucketPolicies(ctx, bucketName)
	api.WriteResp(c, resp, errno)
}
