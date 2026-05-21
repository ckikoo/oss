package auth

import (
	"context"
	"oss/adaptor"
	"oss/api"
	"oss/common"
	"oss/service/dto"
	"oss/service/video"
	"strconv"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
)

type VideoCtrl struct {
	video *video.PlaybackService
}

var _ IVideoHandler = (*VideoCtrl)(nil)

func NewVideoCtrl(adaptor adaptor.IAdaptor) IVideoHandler {
	return &VideoCtrl{video: video.NewPlaybackService(adaptor)}
}

func (ctrl *VideoCtrl) CreatePlayToken(ctx context.Context, c *app.RequestContext) {
	ctx1, pass := common.GetUserInfoFromContext(ctx, c)
	if !pass {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

	req := &dto.CreateVideoPlayTokenReq{}
	if err := c.BindAndValidate(req); err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
		return
	}

	resp, errno := ctrl.video.CreatePlayToken(ctx1, req)
	api.WriteResp(c, resp, errno)
}

func (ctrl *VideoCtrl) GetTranscodeStatus(ctx context.Context, c *app.RequestContext) {
	ctx1, pass := common.GetUserInfoFromContext(ctx, c)
	if !pass {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

	bucketName := c.Param("bucket_name")
	objectKey := c.Param("object_key")
	versionID := c.Query("version_id")
	if bucketName == "" || objectKey == "" {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("bucket_name and object_key are required"))
		return
	}

	bucketName = strings.TrimSpace(bucketName)
	objectKey = strings.TrimSpace(objectKey)
	versionID = strings.TrimSpace(versionID)

	resp, errno := ctrl.video.GetTranscodeStatus(ctx1, bucketName, objectKey, versionID)
	api.WriteResp(c, resp, errno)
}

func (ctrl *VideoCtrl) GetHLSMasterPlaylist(ctx context.Context, c *app.RequestContext) {
	transcodeID, ok := parseTranscodeID(c)
	if !ok {
		writeHLSError(c, common.ParamErr.WithMsg("invalid transcode_id"))
		return
	}

	ctx1, pass := common.GetPlayTokenClaimsFromContext(ctx, c)
	if !pass {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

	content, errno := ctrl.video.GetMasterPlaylist(ctx1, transcodeID)
	writeHLSContent(c, content, errno)
}

func (ctrl *VideoCtrl) GetHLSProfilePlaylist(ctx context.Context, c *app.RequestContext) {
	transcodeID, ok := parseTranscodeID(c)
	if !ok {
		writeHLSError(c, common.ParamErr.WithMsg("invalid transcode_id"))
		return
	}

	ctx1, pass := common.GetPlayTokenClaimsFromContext(ctx, c)
	if !pass {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

	content, errno := ctrl.video.GetProfilePlaylist(ctx1, transcodeID, c.Param("profile"))
	writeHLSContent(c, content, errno)
}

func (ctrl *VideoCtrl) GetHLSSegment(ctx context.Context, c *app.RequestContext) {
	transcodeID, ok := parseTranscodeID(c)
	if !ok {
		writeHLSError(c, common.ParamErr.WithMsg("invalid transcode_id"))
		return
	}
	ctx1, pass := common.GetPlayTokenClaimsFromContext(ctx, c)
	if !pass {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

	content, errno := ctrl.video.GetSegment(ctx1, transcodeID, c.Param("profile"), c.Param("segment"))
	writeHLSContent(c, content, errno)
}

func (ctrl *VideoCtrl) GetHLSKey(ctx context.Context, c *app.RequestContext) {
	keyID := strings.TrimSpace(c.Param("key_id"))
	if keyID == "" {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("key_id is required"))
		return
	}

	ctx1, pass := common.GetPlayTokenClaimsFromContext(ctx, c)
	if !pass {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

	content, errno := ctrl.video.GetKey(ctx1, keyID)
	writeHLSContent(c, content, errno)
}

func parseTranscodeID(c *app.RequestContext) (int64, bool) {
	transcodeID, err := strconv.ParseInt(c.Param("transcode_id"), 10, 64)
	return transcodeID, err == nil && transcodeID > 0
}

func writeHLSContent(c *app.RequestContext, content *video.HLSContent, errno common.Errno) {
	if errno.NotOk() {
		writeHLSError(c, errno)
		return
	}

	if content == nil || content.Body == nil {
		writeHLSError(c, common.ServerErr.WithMsg("empty HLS content"))
		return
	}
	defer content.Body.Close()

	c.Header("Content-Type", content.ContentType)
	c.Header("Cache-Control", "no-store")
	c.Response.SetBodyStream(content.Body, -1)
	// if _, err := io.Copy(c.Response.BodyWriter(), content.Body); err != nil {
	// 	writeHLSError(c, common.ServerErr.WithErr(err))
	// }
}

func writeHLSError(c *app.RequestContext, errno common.Errno) {
	api.WriteResp(c, nil, errno)
}
