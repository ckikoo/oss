package auth

import (
	"context"

	"oss/adaptor"
	"oss/api"
	"oss/common"
	"oss/service/audit"
	"oss/service/dto"

	"github.com/cloudwego/hertz/pkg/app"
)

type AuditCtrl struct {
	service *audit.Service
}

func NewAuditCtrl(adaptor adaptor.IAdaptor) *AuditCtrl {
	return &AuditCtrl{service: audit.NewService(adaptor)}
}

func (ctrl *AuditCtrl) ListOperationLogs(ctx context.Context, c *app.RequestContext) {
	req := &dto.ListOperationLogsReq{}
	if err := c.BindAndValidate(req); err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
		return
	}

	ctx1, pass := common.GetUserInfoFromContext(ctx, c)
	if !pass {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

	resp, errno := ctrl.service.ListOperationLogs(ctx1, req)
	api.WriteResp(c, resp, errno)
}
