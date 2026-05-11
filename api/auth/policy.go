package auth

import (
	"context"
	"oss/adaptor"
	"oss/api"
	"oss/common"
	"oss/service/dto"
	"oss/service/policy"

	"github.com/cloudwego/hertz/pkg/app"
)

type PolicyCtrl struct {
	policy *policy.Service
}

var _ IPolicyHandler = (*PolicyCtrl)(nil)

func NewPolicyCtrl(adaptor adaptor.IAdaptor) IPolicyHandler {
	return &PolicyCtrl{policy: policy.NewService(adaptor)}
}

func (ctrl *PolicyCtrl) CreateBucketPolicy(ctx context.Context, c *app.RequestContext) {

	ctx1, pass := common.GetUserInfoFromContext(ctx, c)
	if !pass {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

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

	resp, errno := ctrl.policy.CreateBucketPolicy(ctx1, bucketName, req)
	api.WriteResp(c, resp, errno)
}

func (ctrl *PolicyCtrl) ListBucketPolicies(ctx context.Context, c *app.RequestContext) {

	ctx1, pass := common.GetUserInfoFromContext(ctx, c)
	if !pass {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

	bucketName := c.Param("bucket_name")
	if bucketName == "" {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("bucket_name is required"))
		return
	}

	resp, errno := ctrl.policy.ListBucketPolicies(ctx1, bucketName)
	api.WriteResp(c, resp, errno)
}
