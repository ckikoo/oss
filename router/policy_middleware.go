package router

import (
	"context"
	"fmt"
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
		if granted, ok := c.Get(consts.TokenGranted); ok && granted.(bool) {
			c.Next(ctx)
			return
		}

		userID := c.GetInt64(consts.UserKeyContext)
		bucketName := c.Param("bucket_name")
		if bucketName == "" {
			c.Next(ctx)
			return
		}

		bucket, err := bRepo.GetByName(ctx, userID, bucketName)
		if err != nil {
			api.WriteResp(c, nil, common.BucketNotFoundErr)
			c.Abort()
			return
		}

		// owner 直接放行
		if bucket.UserID == userID {
			c.Next(ctx)
			return
		}

		ak, _ := c.Get(consts.AccessKeyContext)
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
			Action:   action,
			Resource: resource,
			SourceIP: c.ClientIP(),
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
