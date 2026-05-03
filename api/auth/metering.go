package auth

import (
	"context"

	"oss/api"
	"oss/common"
	"oss/service/dto"
	"oss/service/metering"

	"github.com/cloudwego/hertz/pkg/app"
)

type MeteringCtrl struct {
	service *metering.Service
}

func NewMeteringCtrl(service *metering.Service) *MeteringCtrl {
	return &MeteringCtrl{service: service}
}

func (ctrl *MeteringCtrl) GetDailyMetering(ctx context.Context, c *app.RequestContext) {
	req := &dto.ListDailyMeteringReq{}
	if err := c.BindAndValidate(req); err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
		return
	}

	resp, errno := ctrl.service.ListDailyMetrics(ctx, req)
	api.WriteResp(c, resp, errno)
}
