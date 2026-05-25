package router

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"oss/adaptor"
	bucketgorm "oss/adaptor/repo/bucket/gorm"
	"oss/api"
	"oss/common"
	"oss/consts"
	"oss/service/do"
	"oss/service/policy"

	"github.com/cloudwego/hertz/pkg/app"
)

func NewPolicyMiddleware(adp adaptor.IAdaptor) app.HandlerFunc {
	bRepo := bucketgorm.NewBucketRepo(adp)
	svc := policy.NewService(adp)

	return func(ctx context.Context, c *app.RequestContext) {
		// token 授权的请求直接跳过
		if tokenGranted(c) {
			c.Next(ctx)
			return
		}

		userID := authenticatedUserID(c)
		bucketName := c.Param("bucket_name")
		if bucketName == "" {
			c.Next(ctx)
			return
		}

		bucket, err := loadRouteBucket(ctx, c, bRepo, userID, bucketName)
		if err != nil {
			api.WriteResp(c, nil, common.BucketNotFoundErr)
			c.Abort()
			return
		}
		if bucket == nil {
			api.WriteResp(c, nil, common.BucketNotFoundErr)
			c.Abort()
			return
		}

		// owner 直接放行
		if bucket.UserID == userID {
			c.Next(ctx)
			return
		}

		ak := c.GetString(consts.AccessKeyContext)
		action := resolvePolicyAction(c)
		resource := fmt.Sprintf("arn:oss:::%s/%s",
			bucketName,
			strings.TrimPrefix(c.Param("object_key"), "/"),
		)

		effect := svc.Evaluate(ctx, do.EvaluateReq{
			BucketID: bucket.ID,
			Principals: []string{
				fmt.Sprintf("user:%d", userID),
				fmt.Sprintf("ak:%s", ak),
			},
			Action:     action,
			Resource:   resource,
			SourceIP:   c.ClientIP(),
			UserAgent:  string(c.UserAgent()),
			ObjectTags: parseRequestObjectTags(c),
		})

		switch effect {
		case consts.EffectAllow:
			c.Next(ctx)
		case consts.EffectDeny, consts.EffectNotApplicable:
			c.JSON(403, common.AuthErr.WithMsg("policy denied"))
			c.Abort()
		}
	}
}

func resolvePolicyAction(c *app.RequestContext) string {
	method := string(c.Method())
	p := string(c.Path())

	switch {
	case method == "GET" && strings.Contains(p, "/objects") && strings.HasSuffix(p, "/transcode"):
		return consts.GetTranscodeStatusAction
	case method == "GET" && strings.Contains(p, "/objects"):
		return "GetObject"
	case method == "PUT" && strings.Contains(p, "/objects"):
		return "PutObject"
	case method == "POST" && strings.Contains(p, "/objects"):
		return "PutObject"
	case method == "DELETE" && strings.Contains(p, "/objects"):
		return "DeleteObject"
	case method == "HEAD" && strings.Contains(p, "/objects"):
		return "HeadObject"
	case method == "POST" && strings.Contains(p, "/multipart"):
		return "PutObject"
	case method == "GET" && strings.Contains(p, "/multipart"):
		return "ListMultipartUploads"
	case method == "GET":
		return "ListObjects"
	default:
		return "Unknown"
	}
}

func parseRequestObjectTags(c *app.RequestContext) map[string]string {
	raw := strings.TrimSpace(string(c.GetHeader("X-OSS-Tagging")))
	if raw == "" {
		raw = strings.TrimSpace(string(c.GetHeader("x-oss-tagging")))
	}
	if raw == "" {
		raw = strings.TrimSpace(c.Query("tagging"))
	}
	if raw == "" {
		return nil
	}

	values, err := url.ParseQuery(raw)
	if err != nil {
		return nil
	}

	tags := make(map[string]string, len(values))
	for key, values := range values {
		if len(values) > 0 {
			tags[key] = values[0]
		}
	}
	return tags
}
