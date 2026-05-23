package auth

import (
	"context"
	"oss/adaptor"
	"oss/api"
	healthSvc "oss/service/health"

	"github.com/cloudwego/hertz/pkg/app"
)

type HealthCtrl struct {
	srv healthSvc.IHealthService
}

var _ IHealthHandler = (*HealthCtrl)(nil)

func NewHealthCtrl(adaptor adaptor.IAdaptor) IHealthHandler {
	return &HealthCtrl{
		srv: healthSvc.NewService(adaptor),
	}
}

func (ctrl *HealthCtrl) Healthz(ctx context.Context, c *app.RequestContext) {
	resp, err := ctrl.srv.Liveness(ctx)
	api.WriteResp(c, resp, err)
}

func (ctrl *HealthCtrl) Readyz(ctx context.Context, c *app.RequestContext) {
	resp, err := ctrl.srv.Readiness(ctx)
	api.WriteResp(c, resp, err)
}
