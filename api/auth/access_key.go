package auth

import (
	"context"
	"oss/adaptor"
	"oss/api"
	"oss/common"
	"oss/service/accesskey"
	"oss/service/dto"
	"strconv"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
)

type Ctrl struct {
	auth *accesskey.Service
}

var _ IAccessKeyHandler = (*Ctrl)(nil)

func NewCtrl(adaptor adaptor.IAdaptor) IAccessKeyHandler {
	return &Ctrl{auth: accesskey.NewService(adaptor)}
}

func (ctrl *Ctrl) CreateAccessKey(ctx context.Context, c *app.RequestContext) {
	req := &dto.CreateAccessKeyReq{}
	if err := c.BindAndValidate(req); err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
		return
	}

	resp, errno := ctrl.auth.CreateAccessKey(ctx, req)
	api.WriteResp(c, resp, errno)
}

func (ctrl *Ctrl) ListAccessKeys(ctx context.Context, c *app.RequestContext) {
	ctx1, pass := common.GetUserInfoFromContext(ctx, c)
	if !pass {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

	req := &dto.ListAccessKeysReq{UserID: ctx1.UserID}
	if rawStatus := strings.TrimSpace(c.Query("status")); rawStatus != "" {
		status, err := strconv.ParseInt(rawStatus, 10, 32)
		if err != nil {
			api.WriteResp(c, nil, common.ParamErr.WithMsg("status must be an integer"))
			return
		}
		req.Status = int32(status)
	}

	resp, errno := ctrl.auth.ListAccessKeys(ctx, req)
	api.WriteResp(c, resp, errno)
}

func (ctrl *Ctrl) GetAccessKey(ctx context.Context, c *app.RequestContext) {
	accessKey := c.Param("access_key")
	if accessKey == "" {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("access_key is required"))
		return
	}

	resp, errno := ctrl.auth.GetAccessKey(ctx, accessKey)
	api.WriteResp(c, resp, errno)
}

func (ctrl *Ctrl) DeactivateAccessKey(ctx context.Context, c *app.RequestContext) {
	accessKey := c.Param("access_key")
	if accessKey == "" {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("access_key is required"))
		return
	}

	userCtx, pass := common.GetUserInfoFromContext(ctx, c)
	if !pass {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

	req := &dto.UpdateAccessKeyStatusReq{}
	if err := c.BindAndValidate(req); err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
		return
	}

	resp, errno := ctrl.auth.UpdateAccessKeyStatus(userCtx, accessKey, req)
	api.WriteResp(c, resp, errno)
}
