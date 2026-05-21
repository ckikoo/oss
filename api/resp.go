package api

import (
	"net/http"
	"oss/common"

	"github.com/bytedance/sonic"
	"github.com/cloudwego/hertz/pkg/app"
)

type Resp struct {
	Code   int    `json:"code"`
	Msg    string `json:"msg"`
	ErrMsg string `json:"err_msg"`
	IsAes  bool   `json:"is_aes"`
	Data   any    `json:"data"`
}

// 通用响应函数
func WriteRespUnified(ctx *app.RequestContext, data any, errno common.Errno, isAes bool) {
	resp := Resp{
		Code:   errno.Code,
		Msg:    errno.Msg,
		ErrMsg: errno.ErrMsg,
		IsAes:  isAes,
		Data:   data,
	}

	// 使用 Sonic 序列化
	jsonBytes, err := sonic.Marshal(resp)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, Resp{
			Code: 500,
			Msg:  "Internal JSON Error",
		})
		return
	}

	ctx.Data(http.StatusOK, "application/json", jsonBytes)
}

// 兼容原来的调用方式
func WriteResp(ctx *app.RequestContext, data any, errno common.Errno) {
	WriteRespUnified(ctx, data, errno, false)
}

func WriteRespAes(ctx *app.RequestContext, data any, errno common.Errno) {
	WriteRespUnified(ctx, data, errno, true)
}
