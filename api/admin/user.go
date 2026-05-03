package admin

import (
	"context"
	"oss/api"
	"oss/common"
	"oss/service/dto"

	"github.com/cloudwego/hertz/pkg/app"
)

func (ctrl *Ctrl) CreateUser(ctx context.Context, c *app.RequestContext) {
	req := &dto.CreateUserReq{}

	if err := c.BindAndValidate(req); err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
		return
	}

	id, errno := ctrl.user.CreateUser(ctx, req)

	api.WriteResp(c, map[string]any{
		"id": id,
	}, errno)

}
