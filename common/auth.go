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

const currentActionToken = "current_action_token_"

func GetTokenFromContext(ctx context.Context, c *app.RequestContext) (string, bool) {
	if v, ok := c.Get(currentActionToken); ok {
		if token, ok := v.(string); ok {
			return token, true
		}
	}

	return "", false
}

const PlayTokenClaimsKey = "play_token_claims"

type VideoPlayTokenCtx struct {
	context.Context
	Token       string
	UserID      int64
	BucketID    int64
	BucketName  string
	ObjectID    int64
	ObjectKey   string
	VersionID   string
	TranscodeID int64
	ExpiresAt   int64
	Action      string
}

func GetPlayTokenClaimsFromContext(ctx context.Context, c *app.RequestContext) (*VideoPlayTokenCtx, bool) {
	if v, ok := c.Get(PlayTokenClaimsKey); ok {
		if token, ok := v.(*VideoPlayTokenCtx); ok {
			token.Context = ctx
			return token, true
		}
	}

	return nil, false
}
