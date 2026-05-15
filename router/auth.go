package router

import (
	"context"
	"math"
	"net/url"
	"oss/adaptor"
	"oss/adaptor/repo/accesskey/gorm"
	"oss/api"
	"oss/api/auth"
	"oss/common"
	"oss/consts"
	"oss/utils/tools"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
)

func buildStringToSign(method, path, query, host, contentType, body string, timestamp int64) string {
	var sb strings.Builder

	sb.WriteString(method)
	sb.WriteString(" ")
	sb.WriteString(path)

	if query != "" {
		sb.WriteString("?")
		sb.WriteString(query)
	}

	sb.WriteString("\nHost: ")
	sb.WriteString(host)

	if contentType != "" {
		sb.WriteString("\nContent-Type: ")
		sb.WriteString(contentType)
	}

	sb.WriteString("\nX-OSS-Timestamp: ")
	sb.WriteString(strconv.FormatInt(timestamp, 10))

	sb.WriteString("\n\n")

	if body != "" {
		sb.WriteString(body)
	}

	return sb.String()
}

func canonicalQuery(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}

	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return rawQuery
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	parts := make([]string, 0)

	for _, key := range keys {
		vals := values[key]
		sort.Strings(vals)

		for _, val := range vals {
			parts = append(parts, url.QueryEscape(key)+"="+url.QueryEscape(val))
		}
	}

	return strings.Join(parts, "&")
}
func NewAccessKeyMiddleware(adaptor adaptor.IAdaptor) app.HandlerFunc {
	repo := gorm.NewAccessKeyRepo(adaptor)
	return func(ctx context.Context, c *app.RequestContext) {

		// 特定接口处理
		if string(c.Method()) == "GET" && c.FullPath() == "/api/v1/buckets/:bucket_name/objects/:object_key" {
			token := c.Query("token")

			if token == "" {
				goto NEXT
			}
			ctrl := auth.NewTokenCtrl(adaptor)

			var (
				ak   string
				pass = false
			)

			ak, pass = ctrl.ValidateToken(ctx, token, consts.DownloadAction, c.Param("bucket_name"), c.Param("object_key"))
			if !pass {
				c.JSON(401, common.AuthErr.WithMsg("invalid token"))
				c.Abort()
				return
			}

			info, err := repo.GetByAccessKey(ctx, ak)
			if err != nil {
				c.JSON(500, common.DatabaseErr.WithErr(err))
				c.Abort()
				return
			}

			sec, err := tools.AESDecrypt(info.SecretKey, []byte(adaptor.GetConfig().Security.AESKey))
			if err != nil {
				c.JSON(500, common.ServerErr)
				c.Abort()
				return
			}

			userInfo := &common.UserInfoCtx{Context: ctx, UserID: info.UserID, AccessKey: ak, SecretKey: string(sec)}
			c.Set(consts.UserKeyContext, info.UserID)
			c.Set(consts.UserInfoContext, userInfo)
			c.Set(consts.AccessKeyContext, ak)
			c.Set(consts.SecretKeyContext, string(sec))
			c.Set(consts.TokenGranted, true)
			c.Next(ctx)
			return
		}

	NEXT:

		// 判定是否x-oss-token， 目前先针对分片上传的接口，后续可以根据需要增加其他接口
		if ossToken := string(c.GetHeader(consts.HeaderToken)); ossToken != "" {
			// TODO 校验token
			curPath := string(c.Method()) + " " + c.FullPath()
			action, ok := needTokenCheck[curPath]
			if ok {
				ctrl := auth.NewTokenCtrl(adaptor)

				var (
					ak   string
					pass = false
				)

				ak, pass = ctrl.ValidateToken(ctx, ossToken, action, c.Param("bucket_name"), c.Param("object_key"))
				if !pass {
					c.JSON(401, common.AuthErr.WithMsg("invalid token"))
					c.Abort()
					return
				}

				info, err := repo.GetByAccessKey(ctx, ak)
				if err != nil {
					c.JSON(500, common.DatabaseErr.WithErr(err))
					c.Abort()
					return
				}

				sec, err := tools.AESDecrypt(info.SecretKey, []byte(adaptor.GetConfig().Security.AESKey))
				if err != nil {
					c.JSON(500, common.ServerErr)
					c.Abort()
					return
				}

				userInfo := &common.UserInfoCtx{Context: ctx, UserID: info.UserID, AccessKey: ak, SecretKey: string(sec)}
				c.Set(consts.UserKeyContext, info.UserID)
				c.Set(consts.UserInfoContext, userInfo)
				c.Set(consts.AccessKeyContext, ak)
				c.Set(consts.SecretKeyContext, string(sec))
				c.Set(consts.TokenGranted, true)
				c.Next(ctx)
				return
			}

		}

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

		// 2. 防重放：时间戳偏差不超过 30s
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

		sk, err := tools.AESDecrypt(akInfo.SecretKey, []byte(adaptor.GetConfig().Security.AESKey))
		if err != nil {
			c.JSON(500, common.ServerErr)
			c.Abort()
			return
		}

		contentType := string(c.GetHeader("Content-Type"))
		body := ""

		shouldSkipBody := strings.Contains(contentType, "application/octet-stream") ||
			strings.Contains(contentType, "multipart/")

		if !shouldSkipBody {
			b, err := c.Body()
			if err != nil {
				api.WriteResp(c, nil, common.ReadBodyError)
				c.Abort()
				return
			}
			body = string(b)
		}
		rawQuery := string(c.GetRequest().QueryString())
		query := canonicalQuery(rawQuery)
		stringToSign := buildStringToSign(
			string(c.Method()),
			string(query),
			string(c.GetRequest().QueryString()),
			string(c.Host()),
			contentType,
			body,
			timestamp,
		)

		expectedSignature := tools.HmacSHA256Verify(stringToSign, string(sk), signature)
		if !expectedSignature {
			c.JSON(401, common.AuthErr.WithMsg("invalid signature"))
			c.Abort()
			return
		}

		userInfo := &common.UserInfoCtx{Context: ctx, UserID: akInfo.UserID, AccessKey: akInfo.AccessKey, SecretKey: string(sk)}
		c.Set(consts.SecretKeyContext, sk)
		c.Set(consts.UserKeyContext, akInfo.UserID)
		c.Set(consts.UserInfoContext, userInfo)
		c.Set(consts.AccessKeyContext, akInfo.AccessKey)

		c.Next(ctx)

	}
}
