package auth

import (
	"context"
	"oss/adaptor"
	"oss/api"
	"oss/common"
	"oss/service/accesskey"
	"oss/service/dto"

	"github.com/cloudwego/hertz/pkg/app"
)

type Ctrl struct {
	auth *accesskey.Service
}

func NewCtrl(adaptor adaptor.IAdaptor) *Ctrl {
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
	req := &dto.ListAccessKeysReq{}
	if err := c.BindAndValidate(req); err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
		return
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

	req := &dto.UpdateAccessKeyStatusReq{}
	if err := c.BindAndValidate(req); err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
		return
	}

	resp, errno := ctrl.auth.UpdateAccessKeyStatus(ctx, accessKey, req)
	api.WriteResp(c, resp, errno)
}
