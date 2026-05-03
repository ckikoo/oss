package auth

import (
	"context"
	"math"
	"strconv"
	"strings"
	"time"

	"oss/adaptor"
	"oss/adaptor/repo/accesskey"
	"oss/common"
	"oss/config"
	"oss/consts"
	"oss/utils/tools"

	"github.com/cloudwego/hertz/pkg/app"
)

func buildStringToSign(method, path, query, host, contentType, body string, timestamp int64) string {
	var sb strings.Builder

	sb.WriteString(method + " " + path)
	if query != "" {
		sb.WriteString("?" + query)
	}
	sb.WriteString("\nHost: " + host)
	if contentType != "" {
		sb.WriteString("\nContent-Type: " + contentType)
	}
	sb.WriteString("\n\n")

	// octet-stream 或无 body 直接跳过
	if contentType != "application/octet-stream" && body != "" {
		sb.WriteString(body)
	}

	return sb.String()
}
func NewAccessKeyMiddleware(adaptor adaptor.IAdaptor) app.HandlerFunc {
	repo := accesskey.NewAccessKeyRepo(adaptor)
	return func(ctx context.Context, c *app.RequestContext) {
		auth := string(c.GetHeader("Authorization"))
		if auth == "" {
			c.JSON(401, common.AuthErr)
			c.Abort()
			return
		}

		// 开始校验 Auth
		// OSS AKID123456:1746000000:abc123...
		parts := strings.SplitN(auth, " ", 2)
		if len(parts) != 2 || parts[0] != "OSS" {
			c.JSON(401, common.AuthErr.WithMsg("invalid authorization format"))
			c.Abort()
			return
		}

		fields := strings.Split(parts[1], ":")
		if len(fields) != 3 {
			c.JSON(401, common.AuthErr.WithMsg("invalid authorization fields"))
			c.Abort()
			return
		}

		ak := fields[0]
		timestamp, _ := strconv.ParseInt(fields[1], 10, 64)
		signature := fields[2]

		// 2. 防重放：时间戳偏差不超过 5 分钟
		now := time.Now().Unix()
		if math.Abs(float64(now-timestamp)) > 30 {
			c.JSON(401, common.AuthErr.WithMsg("request expired"))
			c.Abort()
			return
		}

		// 3. 根据 AK 获取 SK 和权限信息
		akInfo, err := repo.GetByAccessKey(ctx, ak)
		if err != nil {
			c.JSON(401, common.AuthErr.WithMsg("invalid access key"))
			c.Abort()
			return
		}

		sk, err := tools.AESDecrypt(akInfo.SecretKey, []byte(config.GlobalConfig.Security.AESKey))
		if err != nil {
			c.JSON(500, common.ServerErr)
			c.Abort()
			return
		}

		contentType := string(c.GetHeader("Content-Type"))
		var body string
		if contentType != "application/octet-stream" {
			b, _ := c.Body()
			body = string(b)
		}

		stringToSign := buildStringToSign(
			string(c.Method()),
			string(c.Path()),
			string(c.GetRequest().QueryString()),
			string(c.Host()),
			contentType,
			body,
			timestamp,
		)

		expectedSignature := tools.Verify(stringToSign, string(sk), signature)
		if !expectedSignature {
			c.JSON(401, common.AuthErr.WithMsg("invalid signature"))
			c.Abort()
			return
		}

		c.Set(consts.UserKeyContext, akInfo.UserID)
		c.Set(consts.AccessKeyContext, akInfo.AccessKey)

		c.Next(ctx)

	}
}
