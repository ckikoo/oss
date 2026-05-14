package router

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
)

func globalCORSMiddleware() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, PUT, POST, DELETE, HEAD, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Oss-Token")

		// preflight 直接返回
		if string(c.Method()) == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next(ctx)
	}
}
