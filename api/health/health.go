package health

import (
	"context"

	"oss/adaptor"
	"oss/api"
	healthSvc "oss/service/health"

	"github.com/cloudwego/hertz/pkg/app"
)

// Ctrl 健康检查 HTTP 处理器
type Ctrl struct {
	service healthSvc.IHealthService
}

// NewHealthCtrl 创建新的健康检查处理器实例
func NewHealthCtrl(adaptor adaptor.IAdaptor) IHealthHandler {
	return &Ctrl{
		service: healthSvc.NewService(adaptor),
	}
}

// Healthz 处理存活性检查请求
// @router GET /healthz
// @response 200 {object} dto.HealthResp
func (ctrl *Ctrl) Healthz(ctx context.Context, c *app.RequestContext) {
	resp, errno := ctrl.service.Liveness(ctx)
	api.WriteResp(c, resp, errno)
}

// Readyz 处理准备就绪检查请求
// @router GET /readyz
// @response 200 {object} dto.HealthResp
// @response 503 {object} dto.HealthResp 服务未准备就绪
func (ctrl *Ctrl) Readyz(ctx context.Context, c *app.RequestContext) {
	resp, errno := ctrl.service.Readiness(ctx)
	api.WriteResp(c, resp, errno)
}
