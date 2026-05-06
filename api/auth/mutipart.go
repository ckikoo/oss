package auth

import (
	"context"
	"oss/api"
	"oss/common"
	"oss/consts"
	"oss/service/dto"
	"oss/service/multipart"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"
)

type multipartCtrl struct {
	object *multipart.Service
}

func NewmultipartCtrl(service *multipart.Service) *multipartCtrl {
	return &multipartCtrl{object: service}
}

func (ctrl *multipartCtrl) CreateMultipartUpload(ctx context.Context, c *app.RequestContext) {
	bucketName := c.Param("bucket_name")
	if bucketName == "" {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("bucket_name is required"))
		return
	}

	req := &dto.CreateMultipartUploadReq{}
	if err := c.BindAndValidate(req); err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
		return
	}

	userId, ok := c.Get(consts.UserKeyContext)
	if !ok {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

	resp, errno := ctrl.object.CreateMultipartUpload(ctx, userId.(int64), bucketName, req)
	api.WriteResp(c, resp, errno)
}

func (ctrl *multipartCtrl) UploadMultipartPart(ctx context.Context, c *app.RequestContext) {
	uploadID := c.Param("upload_id")
	partNumberStr := c.Param("part_number")

	partNumber, err := strconv.Atoi(partNumberStr)
	if err != nil || partNumber <= 0 {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("invalid part_number"))
		return
	}
	// 读取 body 中的二进制数据
	body := c.GetRawData()
	if len(body) == 0 {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("body is empty"))
		return
	}

	etag := c.Request.Header.Get("Content-MD5")

	userId, ok := c.Get(consts.UserKeyContext)
	if !ok {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}
	resp, errno := ctrl.object.UploadMultipartPart(ctx, userId.(int64), etag, uploadID, int32(partNumber), body)
	api.WriteResp(c, resp, errno)
}

func (ctrl *multipartCtrl) CompleteMultipartUpload(ctx context.Context, c *app.RequestContext) {
	bucketName := c.Param("bucket_name")
	uploadID := c.Param("upload_id")
	if bucketName == "" || uploadID == "" {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("bucket_name and upload_id are required"))
		return
	}

	req := &dto.CompleteMultipartUploadReq{}
	if err := c.BindAndValidate(req); err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
		return
	}

	userId, ok := c.Get(consts.UserKeyContext)
	if !ok {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

	resp, errno := ctrl.object.CompleteMultipartUpload(ctx, userId.(int64), uploadID, req)
	api.WriteResp(c, resp, errno)
}

func (ctrl *multipartCtrl) AbortMultipartUpload(ctx context.Context, c *app.RequestContext) {
	bucketName := c.Param("bucket_name")
	uploadID := c.Param("upload_id")
	if bucketName == "" || uploadID == "" {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("bucket_name and upload_id are required"))
		return
	}

	userId, ok := c.Get(consts.UserKeyContext)
	if !ok {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

	errno := ctrl.object.AbortMultipartUpload(ctx, userId.(int64), uploadID)
	api.WriteResp(c, nil, errno)
}
