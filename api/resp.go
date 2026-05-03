package api

import (
	"net/http"
	"oss/common"

	"github.com/cloudwego/hertz/pkg/app"
)

type Resp struct {
	Code   int    `json:"code"`
	Msg    string `json:"msg"`
	ErrMsg string `json:"err_msg"`
	IsAes  bool   `json:"is_aes"`
	Data   any    `json:"data"`
}

func WriteResp(ctx *app.RequestContext, data any, errno common.Errno) {
	ctx.JSON(http.StatusOK, Resp{
		Code:   errno.Code,
		Msg:    errno.Msg,
		ErrMsg: errno.ErrMsg,
		Data:   data,
	})
}

func WriteRespAes(ctx *app.RequestContext, data any, errno common.Errno) {
	ctx.JSON(http.StatusOK, Resp{
		Code:   errno.Code,
		Msg:    errno.Msg,
		ErrMsg: errno.ErrMsg,
		IsAes:  true,
		Data:   data,
	})
}
