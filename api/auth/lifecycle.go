package auth

import (
	"context"
	"strconv"

	"oss/api"
	"oss/common"
	"oss/service/dto"
	"oss/service/lifecycle"

	"github.com/cloudwego/hertz/pkg/app"
)

type LifecycleCtrl struct {
	lifecycle *lifecycle.Service
}

func NewLifecycleCtrl(service *lifecycle.Service) *LifecycleCtrl {
	return &LifecycleCtrl{lifecycle: service}
}

func (ctrl *LifecycleCtrl) CreateLifecycleRule(ctx context.Context, c *app.RequestContext) {
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

	resp, errno := ctrl.lifecycle.CreateLifecycleRule(ctx, bucketName, req)
	api.WriteResp(c, resp, errno)
}

func (ctrl *LifecycleCtrl) ListLifecycleRules(ctx context.Context, c *app.RequestContext) {
	bucketName := c.Param("bucket_name")
	if bucketName == "" {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("bucket_name is required"))
		return
	}

	resp, errno := ctrl.lifecycle.ListLifecycleRules(ctx, bucketName)
	api.WriteResp(c, resp, errno)
}

func (ctrl *LifecycleCtrl) GetLifecycleRule(ctx context.Context, c *app.RequestContext) {
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

	resp, errno := ctrl.lifecycle.GetLifecycleRule(ctx, bucketName, ruleID)
	api.WriteResp(c, resp, errno)
}

func (ctrl *LifecycleCtrl) UpdateLifecycleRule(ctx context.Context, c *app.RequestContext) {
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

	resp, errno := ctrl.lifecycle.UpdateLifecycleRule(ctx, bucketName, ruleID, req)
	api.WriteResp(c, resp, errno)
}

func (ctrl *LifecycleCtrl) DeleteLifecycleRule(ctx context.Context, c *app.RequestContext) {
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

	errno := ctrl.lifecycle.DeleteLifecycleRule(ctx, bucketName, ruleID)
	api.WriteResp(c, nil, errno)
}
