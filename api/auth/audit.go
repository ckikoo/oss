package auth

import (
	"context"

	"oss/api"
	"oss/common"
	"oss/service/audit"
	"oss/service/dto"

	"github.com/cloudwego/hertz/pkg/app"
)

type AuditCtrl struct {
	service *audit.Service
}

func NewAuditCtrl(service *audit.Service) *AuditCtrl {
	return &AuditCtrl{service: service}
}

func (ctrl *AuditCtrl) ListOperationLogs(ctx context.Context, c *app.RequestContext) {
	req := &dto.ListOperationLogsReq{}
	if err := c.BindAndValidate(req); err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
		return
	}

	resp, errno := ctrl.service.ListOperationLogs(ctx, req)
	api.WriteResp(c, resp, errno)
}
