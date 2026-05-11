package auth

import (
	"context"
	"strconv"

	"oss/adaptor"
	"oss/api"
	"oss/common"
	"oss/service/dto"
	"oss/service/lifecycle"

	"github.com/cloudwego/hertz/pkg/app"
)

type LifecycleCtrl struct {
	lifecycle *lifecycle.Service
}

var _ ILifecycleHandler = (*LifecycleCtrl)(nil)

func NewLifecycleCtrl(adaptor adaptor.IAdaptor) ILifecycleHandler {
	return &LifecycleCtrl{lifecycle: lifecycle.NewService(adaptor)}
}

func (ctrl *LifecycleCtrl) CreateLifecycleRule(ctx context.Context, c *app.RequestContext) {
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

	req := &dto.CreateLifecycleRuleReq{}
	if err := c.BindAndValidate(req); err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
		return
	}

	resp, errno := ctrl.lifecycle.CreateLifecycleRule(ctx1, bucketName, req)
	api.WriteResp(c, resp, errno)
}

func (ctrl *LifecycleCtrl) ListLifecycleRules(ctx context.Context, c *app.RequestContext) {
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

	resp, errno := ctrl.lifecycle.ListLifecycleRules(ctx1, bucketName)
	api.WriteResp(c, resp, errno)
}

func (ctrl *LifecycleCtrl) GetLifecycleRule(ctx context.Context, c *app.RequestContext) {
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

	ruleID, err := strconv.ParseInt(c.Param("rule_id"), 10, 64)
	if err != nil || ruleID <= 0 {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("rule_id is required"))
		return
	}

	resp, errno := ctrl.lifecycle.GetLifecycleRule(ctx1, bucketName, ruleID)
	api.WriteResp(c, resp, errno)
}

func (ctrl *LifecycleCtrl) UpdateLifecycleRule(ctx context.Context, c *app.RequestContext) {
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

	ruleID, err := strconv.ParseInt(c.Param("rule_id"), 10, 64)
	if err != nil || ruleID <= 0 {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("rule_id is required"))
		return
	}

	req := &dto.UpdateLifecycleRuleReq{}
	if err := c.BindAndValidate(req); err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
		return
	}

	resp, errno := ctrl.lifecycle.UpdateLifecycleRule(ctx1, bucketName, ruleID, req)
	api.WriteResp(c, resp, errno)
}

func (ctrl *LifecycleCtrl) DeleteLifecycleRule(ctx context.Context, c *app.RequestContext) {
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

	ruleID, err := strconv.ParseInt(c.Param("rule_id"), 10, 64)
	if err != nil || ruleID <= 0 {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("rule_id is required"))
		return
	}

	errno := ctrl.lifecycle.DeleteLifecycleRule(ctx1, bucketName, ruleID)
	api.WriteResp(c, nil, errno)
}
