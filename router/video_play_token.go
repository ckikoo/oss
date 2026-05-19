package router

import (
	"context"

	"oss/adaptor"
	"oss/adaptor/redis"

	"github.com/cloudwego/hertz/pkg/app"
)

// NewVideoPlayTokenMiddleware validates playback tokens for HLS playlist,
// segment and key-server requests only. Management APIs continue to use
// AccessKey/HMAC plus policy/ACL middleware.
func NewVideoPlayTokenMiddleware(adaptor adaptor.IAdaptor) app.HandlerFunc {
	handler := &playTokenHandler{playToken: redis.NewPlayToken(adaptor)}

	return func(ctx context.Context, c *app.RequestContext) {
		if string(c.Method()) == "OPTIONS" {
			c.Next(ctx)
			return
		}

		handler.Handle(ctx, c, func() {
			c.Next(ctx)
		})
	}
}
