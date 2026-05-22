package auth

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
)

type IVideoHandler interface {
	CreatePlayToken(ctx context.Context, c *app.RequestContext)
	GetTranscodeStatus(ctx context.Context, c *app.RequestContext)
	GetHLSMasterPlaylist(ctx context.Context, c *app.RequestContext)
	GetHLSProfilePlaylist(ctx context.Context, c *app.RequestContext)
	GetHLSSegment(ctx context.Context, c *app.RequestContext)
	GetHLSKey(ctx context.Context, c *app.RequestContext)
}
