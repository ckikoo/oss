package router

import (
	"context"
	"strconv"
	"strings"

	"oss/adaptor"
	"oss/common"
	"oss/config"
	corssvc "oss/service/cors"

	"github.com/cloudwego/hertz/pkg/app"
)

const defaultCORSHeaders = "Authorization, Content-Type, X-OSS-Token, X-Oss-Token, X-Play-Token"

func newAuthenticatedCORSMiddleware(adaptor adaptor.IAdaptor) app.HandlerFunc {
	corsService := corssvc.NewService(adaptor)

	return func(ctx context.Context, c *app.RequestContext) {
		origin := strings.TrimSpace(string(c.GetHeader("Origin")))
		if origin == "" {
			c.Next(ctx)
			return
		}

		corsConf := adaptor.GetConfig().CORS
		if isPreflight(c) {
			setDefaultPreflightCORSHeaders(c, corsConf)
			c.AbortWithStatus(204)
			return
		}

		requestMethod := corsRequestMethod(c)
		bucketName := corsBucketName(c)
		if bucketName != "" {
			userInfo, pass := common.GetUserInfoFromContext(ctx, c)
			if !pass {
				abortCORS(c, common.AuthErr)
				return
			}

			result, errno := corsService.CheckBucketCors(ctx, userInfo.UserID, bucketName, origin, requestMethod)
			if errno.NotOk() {
				abortCORS(c, errno)
				return
			}

			headers := corsRequestHeaders(c)
			if headers == "" {
				headers = defaultCORSHeaders
			}

			setCORSHeaders(c, result.AllowedOrigin, result.AllowedMethods, headers, result.MaxAgeSeconds)
			c.Next(ctx)
			return
		}

		allowedOrigin, ok := matchGlobalOrigin(corsConf, origin)
		if !ok {
			abortCORS(c, common.PermissionErr.WithMsg("origin is not in global cors whitelist"))
			return
		}

		setCORSHeaders(c, allowedOrigin, globalAllowedMethods(corsConf), strings.Join(globalAllowedHeaders(corsConf), ", "), globalMaxAge(corsConf))
		c.Next(ctx)
	}
}

func isPreflight(c *app.RequestContext) bool {
	return strings.EqualFold(string(c.Method()), "OPTIONS")
}

func corsRequestMethod(c *app.RequestContext) string {
	method := strings.TrimSpace(string(c.GetHeader("Access-Control-Request-Method")))
	if method == "" {
		method = string(c.Method())
	}
	return strings.ToUpper(method)
}

func corsRequestHeaders(c *app.RequestContext) string {
	return strings.TrimSpace(string(c.GetHeader("Access-Control-Request-Headers")))
}

func corsBucketName(c *app.RequestContext) string {
	bucketName := strings.TrimSpace(c.Param("bucket_name"))
	if bucketName != "" {
		return bucketName
	}

	parts := strings.Split(strings.Trim(string(c.Path()), "/"), "/")
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "buckets" {
			return strings.TrimSpace(parts[i+1])
		}
	}
	return ""
}

func matchGlobalOrigin(conf config.CORS, origin string) (string, bool) {
	for _, allowed := range conf.AllowedOrigins {
		allowed = strings.TrimSpace(allowed)
		if allowed == "*" {
			return "*", true
		}
		if strings.EqualFold(allowed, origin) {
			return origin, true
		}
	}
	return "", false
}

func globalAllowedMethods(conf config.CORS) []string {
	if len(conf.AllowedMethods) == 0 {
		return []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}
	}

	methods := make([]string, 0, len(conf.AllowedMethods))
	for _, method := range conf.AllowedMethods {
		method = strings.ToUpper(strings.TrimSpace(method))
		if method != "" {
			methods = append(methods, method)
		}
	}
	return methods
}

func globalAllowedHeaders(conf config.CORS) []string {
	if len(conf.AllowedHeaders) == 0 {
		return []string{"Authorization", "Content-Type", "X-OSS-Token", "X-Oss-Token", "X-Play-Token"}
	}

	headers := make([]string, 0, len(conf.AllowedHeaders))
	for _, header := range conf.AllowedHeaders {
		header = strings.TrimSpace(header)
		if header != "" {
			headers = append(headers, header)
		}
	}
	return headers
}

func globalMaxAge(conf config.CORS) int32 {
	if conf.MaxAgeSeconds <= 0 {
		return 600
	}
	return conf.MaxAgeSeconds
}

func setDefaultPreflightCORSHeaders(c *app.RequestContext, conf config.CORS) {
	headers := corsRequestHeaders(c)
	if headers == "" {
		headers = strings.Join(globalAllowedHeaders(conf), ", ")
	}
	setCORSHeaders(c, "*", globalAllowedMethods(conf), headers, globalMaxAge(conf))
}

func setCORSHeaders(c *app.RequestContext, origin string, methods []string, headers string, maxAge int32) {
	if maxAge <= 0 {
		maxAge = 600
	}
	c.Header("Vary", "Origin, Access-Control-Request-Method, Access-Control-Request-Headers")
	c.Header("Access-Control-Allow-Origin", origin)
	c.Header("Access-Control-Allow-Methods", strings.Join(methods, ", "))
	c.Header("Access-Control-Allow-Headers", headers)
	c.Header("Access-Control-Max-Age", strconv.FormatInt(int64(maxAge), 10))
}

func abortCORS(c *app.RequestContext, errno common.Errno) {
	c.JSON(403, errno)
	c.Abort()
}
