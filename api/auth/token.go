package auth

import (
	"context"
	"oss/adaptor"
	"oss/api"
	"oss/common"
	"oss/consts"
	"oss/service/dto"
	"oss/service/token"

	"github.com/cloudwego/hertz/pkg/app"
)

type TokenCtrl struct {
	srv *token.Service
}

func NewTokenCtrl(adaptor adaptor.IAdaptor) *TokenCtrl {
	return &TokenCtrl{
		srv: token.NewService(adaptor),
	}
}

func (ctrl *TokenCtrl) CreateUploadToken(ctx context.Context, c *app.RequestContext) {

	ak := c.GetString(consts.AccessKeyContext)
	secure := c.GetString(consts.SecretKeyContext)

	req := &dto.CreateUploadTokenReq{}

	if err := c.BindAndValidate(req); err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
		return
	}

	resp, errno := ctrl.srv.CreateUploadToken(ctx, ak, secure, req)
	if errno.NotOk() {
		api.WriteResp(c, nil, errno)
		return
	}

	api.WriteResp(c, resp, errno)

}
func (ctrl *TokenCtrl) CreateDownloadToken(ctx context.Context, c *app.RequestContext) {

	ak := c.GetString(consts.AccessKeyContext)
	secure := c.GetString(consts.SecretKeyContext)
	req := &dto.CreateDownloadTokenReq{}

	if err := c.BindAndValidate(&req); err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
		return
	}

	resp, errno := ctrl.srv.CreateDownloadToken(ctx, ak, secure, req)
	if errno.NotOk() {
		api.WriteResp(c, nil, errno)
		return
	}

	api.WriteResp(c, resp, errno)
}
func (ctrl *TokenCtrl) ValidateToken(ctx context.Context, token, action string) (ak string, pass bool) {

	return ctrl.srv.ValidateToken(ctx, token, action)
}
