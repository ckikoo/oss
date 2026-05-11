package auth

import (
	"context"

	"oss/adaptor"
	"oss/api"
	"oss/common"
	"oss/service/dto"
	"oss/service/metering"

	"github.com/cloudwego/hertz/pkg/app"
)

type MeteringHandler struct {
	service *metering.Service
}

var _ IMeteringHandler = (*MeteringHandler)(nil)

func NewMeteringCtrl(adaptor adaptor.IAdaptor) IMeteringHandler {
	return &MeteringHandler{service: metering.NewService(adaptor)}
}
func (ctrl *MeteringHandler) GetDailyMetering(ctx context.Context, c *app.RequestContext) {
	req := &dto.ListDailyMeteringReq{}
	if err := c.BindAndValidate(req); err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
		return
	}

	ctx1, pass := common.GetUserInfoFromContext(ctx, c)
	if !pass {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

	resp, errno := ctrl.service.ListDailyMetrics(ctx1, req)
	api.WriteResp(c, resp, errno)
}
