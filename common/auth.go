package common

import (
	"context"
	"oss/consts"

	"github.com/cloudwego/hertz/pkg/app"
)

type UserInfoCtx struct {
	context.Context
	UserID    int64
	AccessKey string
	SecretKey string
}

func SetContextWithUserInfo(ctx context.Context, userInfo *UserInfoCtx) context.Context {
	return context.WithValue(ctx, consts.UserInfoContext, userInfo)
}

func GetUserInfoFromContext(ctx context.Context, c *app.RequestContext) (*UserInfoCtx, bool) {
	if v, ok := c.Get(consts.UserInfoContext); ok {
		if userInfo, ok := v.(*UserInfoCtx); ok {
			userInfo.Context = ctx
			return userInfo, true
		}
	}

	userID := c.GetInt64(consts.UserKeyContext)
	if userID == 0 {
		return nil, false
	}

	return &UserInfoCtx{
		Context:   ctx,
		UserID:    userID,
		AccessKey: c.GetString(consts.AccessKeyContext),
		SecretKey: c.GetString(consts.SecretKeyContext),
	}, true

}
