package auth

import (
	"context"

	"oss/api"
	"oss/common"
	"oss/consts"
	"oss/service/dto"
	"oss/service/presigned"

	"github.com/cloudwego/hertz/pkg/app"
)

type PresignedCtrl struct {
	presigned *presigned.Service
}

func NewPresignedCtrl(service *presigned.Service) *PresignedCtrl {
	return &PresignedCtrl{presigned: service}
}

func (ctrl *PresignedCtrl) CreatePresignedUrl(ctx context.Context, c *app.RequestContext) {
	uidAny, ok := c.Get(consts.UserKeyContext)
	if !ok {
		api.WriteResp(c, nil, common.AuthErr.WithMsg("user context missing"))
		return
	}
	userID, ok := uidAny.(int64)
	if !ok {
		api.WriteResp(c, nil, common.AuthErr.WithMsg("invalid user context"))
		return
	}

	req := &dto.CreatePresignedUrlReq{}
	if err := c.BindAndValidate(req); err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
		return
	}

	resp, errno := ctrl.presigned.CreatePresignedUrl(ctx, userID, req)
	api.WriteResp(c, resp, errno)
}

func (ctrl *PresignedCtrl) RevokePresignedUrl(ctx context.Context, c *app.RequestContext) {
	uidAny, ok := c.Get(consts.UserKeyContext)
	if !ok {
		api.WriteResp(c, nil, common.AuthErr.WithMsg("user context missing"))
		return
	}
	userID, ok := uidAny.(int64)
	if !ok {
		api.WriteResp(c, nil, common.AuthErr.WithMsg("invalid user context"))
		return
	}

	token := c.Param("token")
	if token == "" {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("token is required"))
		return
	}

	errno := ctrl.presigned.RevokePresignedUrl(ctx, userID, token)
	if errno.NotOk() {
		api.WriteResp(c, nil, errno)
		return
	}

	api.WriteResp(c, map[string]bool{"success": true}, errno)
}
