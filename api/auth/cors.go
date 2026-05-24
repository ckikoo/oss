package auth

import (
	"context"
	"strconv"

	"oss/adaptor"
	"oss/api"
	"oss/common"
	corssvc "oss/service/cors"
	"oss/service/dto"

	"github.com/cloudwego/hertz/pkg/app"
)

type CorsCtrl struct {
	cors *corssvc.Service
}

var _ ICorsHandler = (*CorsCtrl)(nil)

func NewCorsCtrl(adaptor adaptor.IAdaptor) ICorsHandler {
	return &CorsCtrl{cors: corssvc.NewService(adaptor)}
}

func (ctrl *CorsCtrl) CreateBucketCorsRule(ctx context.Context, c *app.RequestContext) {
	bucketName := c.Param("bucket_name")
	req := &dto.CreateBucketCorsRuleReq{}
	if err := c.BindAndValidate(req); err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
		return
	}

	userCtx, pass := common.GetUserInfoFromContext(ctx, c)
	if !pass {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

	resp, errno := ctrl.cors.CreateBucketCorsRule(userCtx, bucketName, req)
	api.WriteResp(c, resp, errno)
}

func (ctrl *CorsCtrl) ListBucketCorsRules(ctx context.Context, c *app.RequestContext) {
	bucketName := c.Param("bucket_name")
	userCtx, pass := common.GetUserInfoFromContext(ctx, c)
	if !pass {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

	resp, errno := ctrl.cors.ListBucketCorsRules(userCtx, bucketName)
	api.WriteResp(c, resp, errno)
}

func (ctrl *CorsCtrl) UpdateBucketCorsRule(ctx context.Context, c *app.RequestContext) {
	bucketName := c.Param("bucket_name")
	ruleID, errno := parseRuleID(c.Param("rule_id"))
	if errno.NotOk() {
		api.WriteResp(c, nil, errno)
		return
	}

	req := &dto.UpdateBucketCorsRuleReq{}
	if err := c.BindAndValidate(req); err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
		return
	}

	userCtx, pass := common.GetUserInfoFromContext(ctx, c)
	if !pass {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

	resp, errno := ctrl.cors.UpdateBucketCorsRule(userCtx, bucketName, ruleID, req)
	api.WriteResp(c, resp, errno)
}

func (ctrl *CorsCtrl) DeleteBucketCorsRule(ctx context.Context, c *app.RequestContext) {
	bucketName := c.Param("bucket_name")
	ruleID, errno := parseRuleID(c.Param("rule_id"))
	if errno.NotOk() {
		api.WriteResp(c, nil, errno)
		return
	}

	userCtx, pass := common.GetUserInfoFromContext(ctx, c)
	if !pass {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

	api.WriteResp(c, nil, ctrl.cors.DeleteBucketCorsRule(userCtx, bucketName, ruleID))
}

func parseRuleID(raw string) (int64, common.Errno) {
	ruleID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || ruleID <= 0 {
		return 0, common.ParamErr.WithMsg("invalid rule_id")
	}
	return ruleID, common.OK
}
