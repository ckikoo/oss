package auth

import (
	"context"
	"oss/adaptor"
	"oss/api"
	"oss/common"
	"oss/service/dto"
	"oss/service/event"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"
)

type EventCtrl struct {
	event *event.Service
}

func NewEventCtrl(adaptor adaptor.IAdaptor) *EventCtrl {
	return &EventCtrl{
		event: event.NewService(adaptor),
	}
}

// CreateEventRule 创建事件规则
func (ctrl *EventCtrl) CreateEventRule(ctx context.Context, c *app.RequestContext) {
	var req dto.CreateEventRuleReq
	if err := c.BindAndValidate(&req); err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
		return
	}

	ctx1, pass := common.GetUserInfoFromContext(ctx, c)
	if !pass {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

	resp, errno := ctrl.event.CreateEventRule(ctx1, &req)

	api.WriteResp(c, resp, errno)
}

// ListEventRules 列出事件规则
func (ctrl *EventCtrl) ListEventRules(ctx context.Context, c *app.RequestContext) {
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
	resp, errno := ctrl.event.ListEventRules(ctx1, bucketName)

	api.WriteResp(c, resp, errno)
}

// UpdateEventRule 更新事件规则
func (ctrl *EventCtrl) UpdateEventRule(ctx context.Context, c *app.RequestContext) {
	ruleIDStr := c.Param("rule_id")
	ruleID, err := strconv.ParseInt(ruleIDStr, 10, 64)
	if err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
		return
	}
	bucketName := c.Param("bucket_name")
	if bucketName == "" {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("bucket_name is required"))
		return
	}

	var req dto.UpdateEventRuleReq
	if err := c.BindAndValidate(&req); err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
		return
	}

	ctx1, pass := common.GetUserInfoFromContext(ctx, c)
	if !pass {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}
	errno := ctrl.event.UpdateEventRule(ctx1, bucketName, ruleID, &req)
	api.WriteResp(c, nil, errno)
}

// DeleteEventRule 删除事件规则
func (ctrl *EventCtrl) DeleteEventRule(ctx context.Context, c *app.RequestContext) {
	ruleIDStr := c.Param("rule_id")
	ruleID, err := strconv.ParseInt(ruleIDStr, 10, 64)
	if err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
		return
	}

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

	errno := ctrl.event.DeleteEventRule(ctx1, bucketName, ruleID)
	api.WriteResp(c, nil, errno)
}
