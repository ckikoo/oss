package auth

import (
	"context"
	"strings"

	"oss/adaptor"
	"oss/adaptor/repo/accesskey"
	"oss/api"
	"oss/common"
	"oss/consts"
	"oss/utils/tools"

	"github.com/cloudwego/hertz/pkg/app"
)

func NewAccessKeyMiddleware(adaptor adaptor.IAdaptor) app.HandlerFunc {
	repo := accesskey.NewAccessKeyRepo(adaptor)
	return func(ctx context.Context, c *app.RequestContext) {
		accessKey := strings.TrimSpace(string(c.GetHeader("X-Access-Key")))
		secretKey := strings.TrimSpace(string(c.GetHeader("X-Secret-Key")))
		if accessKey == "" || secretKey == "" {
			authHeader := string(c.GetHeader("Authorization"))
			if authHeader != "" {
				parts := strings.Fields(authHeader)
				if len(parts) == 2 && strings.EqualFold(parts[0], "AccessKey") {
					pair := strings.SplitN(parts[1], ":", 2)
					if len(pair) == 2 {
						accessKey = strings.TrimSpace(pair[0])
						secretKey = strings.TrimSpace(pair[1])
					}
				}
			}
		}

		if accessKey == "" || secretKey == "" {
			api.WriteResp(c, nil, common.AuthErr.WithMsg("missing X-Access-Key or X-Secret-Key"))
			return
		}

		secretHash := tools.Sha256Hash(secretKey)

		if info, err := repo.GetByAccessKey(ctx, accessKey); err == nil {
			if info.SecretKey != secretHash {
				api.WriteResp(c, nil, common.AuthErr.WithMsg("invalid access key or secret"))
				return
			}
			c.Set(consts.UserKeyContext, info.UserID)
			c.Next(ctx)
		} else {
			api.WriteResp(c, nil, common.AuthErr.WithMsg("invalid access key or secret"))
			return
		}

	}
}
