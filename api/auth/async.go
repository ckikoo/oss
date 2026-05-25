package auth

import (
	"context"
	"strconv"

	"oss/adaptor"
	"oss/api"
	"oss/common"
	"oss/consts"
	asyncSvc "oss/service/async"
	"oss/service/dto"

	"github.com/cloudwego/hertz/pkg/app"
)

type AsyncCtrl struct {
	service *asyncSvc.Service
}

var _ IAsyncHandler = (*AsyncCtrl)(nil)

func NewAsyncCtrl(adaptor adaptor.IAdaptor) IAsyncHandler {
	return &AsyncCtrl{service: asyncSvc.NewService(adaptor)}
}

func (ctrl *AsyncCtrl) ListTasks(ctx context.Context, c *app.RequestContext) {
	req := &dto.ListAsyncTasksReq{}
	if err := c.BindAndValidate(req); err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
		return
	}
	if req.TaskType != "" && !consts.ValidAsyncTaskType(req.TaskType) {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("invalid task_type"))
		return
	}
	if req.Status != nil && !consts.ValidAsyncTaskStatus(*req.Status) {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("invalid status"))
		return
	}

	ctx1, pass := common.GetUserInfoFromContext(ctx, c)
	if !pass {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

	resp, errno := ctrl.service.ListTasks(ctx1, req)
	api.WriteResp(c, resp, errno)
}

func (ctrl *AsyncCtrl) GetTask(ctx context.Context, c *app.RequestContext) {
	ctx1, taskID, ok := ctrl.parseTaskRequest(ctx, c)
	if !ok {
		return
	}
	resp, errno := ctrl.service.GetTask(ctx1, taskID)
	api.WriteResp(c, resp, errno)
}

func (ctrl *AsyncCtrl) RetryTask(ctx context.Context, c *app.RequestContext) {
	ctx1, taskID, ok := ctrl.parseTaskRequest(ctx, c)
	if !ok {
		return
	}
	resp, errno := ctrl.service.RetryTask(ctx1, taskID)
	api.WriteResp(c, resp, errno)
}

func (ctrl *AsyncCtrl) CancelTask(ctx context.Context, c *app.RequestContext) {
	ctx1, taskID, ok := ctrl.parseTaskRequest(ctx, c)
	if !ok {
		return
	}
	resp, errno := ctrl.service.CancelTask(ctx1, taskID)
	api.WriteResp(c, resp, errno)
}

func (ctrl *AsyncCtrl) parseTaskRequest(ctx context.Context, c *app.RequestContext) (*common.UserInfoCtx, int64, bool) {
	ctx1, pass := common.GetUserInfoFromContext(ctx, c)
	if !pass {
		api.WriteResp(c, nil, common.AuthErr)
		return nil, 0, false
	}

	taskID, err := strconv.ParseInt(c.Param("task_id"), 10, 64)
	if err != nil || taskID <= 0 {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("invalid task_id"))
		return nil, 0, false
	}
	return ctx1, taskID, true
}
