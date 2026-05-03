package auth

import (
	"context"
	"oss/api"
	"oss/common"
	"oss/consts"
	"oss/service/dto"
	"oss/service/mutipart"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"
)

type MutipartCtrl struct {
	object *mutipart.Service
}

func NewMutipartCtrl(service *mutipart.Service) *MutipartCtrl {
	return &MutipartCtrl{object: service}
}

func (ctrl *MutipartCtrl) CreateMultipartUpload(ctx context.Context, c *app.RequestContext) {
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

func (ctrl *MutipartCtrl) UploadMultipartPart(ctx context.Context, c *app.RequestContext) {
	uploadID := c.Param("upload_id")
	partNumberStr := c.Param("part_number")
	etag := c.Param("etag")
	partNumber, err := strconv.Atoi(partNumberStr)
	if err != nil || partNumber <= 0 {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("invalid part_number"))
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("file is required"))
		return
	}

	userId, ok := c.Get(consts.UserKeyContext)
	if !ok {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

	resp, errno := ctrl.object.UploadMultipartPart(ctx, userId.(int64), etag, uploadID, int32(partNumber), file)
	api.WriteResp(c, resp, errno)
}

func (ctrl *MutipartCtrl) CompleteMultipartUpload(ctx context.Context, c *app.RequestContext) {
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

func (ctrl *MutipartCtrl) AbortMultipartUpload(ctx context.Context, c *app.RequestContext) {
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
