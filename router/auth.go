package router

import (
	"context"
	"errors"
	"math"
	"net/url"
	"oss/adaptor"
	"oss/adaptor/redis"
	akRepo "oss/adaptor/repo/accesskey"
	akGorm "oss/adaptor/repo/accesskey/gorm"
	"oss/api"
	ossAuth "oss/api/auth"
	"oss/common"
	"oss/config"
	"oss/consts"
	"oss/service/do"
	"oss/utils/tools"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
)

// ============================================================
// 工具函数（签名构建，与原始逻辑保持一致）
// ============================================================

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

// ============================================================
// 责任链接口
// ============================================================

// authHandler 是认证责任链的节点接口。
// 每个实现只负责一种认证方式：
//   - 能处理 → 验证并写入 Context，调用 c.Next / c.Abort。
//   - 不能处理 → 原封不动地调用 next()，将控制权传递给下一个节点。
type authHandler interface {
	Handle(ctx context.Context, c *app.RequestContext, next func())
}

// ============================================================
// 共享辅助函数（消除三条路径的重复代码）
// ============================================================

// lookupAndDecryptSK 根据 AK 查询数据库记录并解密 SecretKey。
// 三种认证方式都需要这两步，统一在此处理。
func lookupAndDecryptSK(
	ctx context.Context,
	repo akRepo.IAccessKeyRepo,
	aesKey string,
	ak string,
) (*do.AccessKeyDo, string, error) {
	info, err := repo.GetByAccessKey(ctx, ak)
	if err != nil {
		return nil, "", err
	}

	key, err := config.Security{AESKey: aesKey}.AESKeyBytes()
	if err != nil {
		return nil, "", err
	}

	sk, err := tools.AESDecrypt(info.SecretKey, key)
	if err != nil {
		return nil, "", err
	}

	return info, string(sk), nil
}

// setUserContext 将认证结果写入 Hertz 请求上下文，供后续 Handler 使用。
func setUserContext(ctx context.Context, c *app.RequestContext, info *do.AccessKeyDo, sk string) {
	userInfo := &common.UserInfoCtx{
		Context:   ctx,
		UserID:    info.UserID,
		AccessKey: info.AccessKey,
		SecretKey: sk,
	}
	c.Set(consts.UserKeyContext, info.UserID)
	c.Set(consts.UserInfoContext, userInfo)
	c.Set(consts.AccessKeyContext, info.AccessKey)
	c.Set(consts.SecretKeyContext, sk)
}

// ============================================================
// Handler 1：下载 Token（URL ?token= 查询参数）
// ============================================================

// downloadTokenHandler 处理通过 URL ?token= 传入临时下载令牌的请求。
// 仅匹配：GET /api/v1/buckets/:bucket_name/objects/:object_key 且 token 非空。
type downloadTokenHandler struct {
	repo    akRepo.IAccessKeyRepo
	adaptor adaptor.IAdaptor
}

func (h *downloadTokenHandler) Handle(ctx context.Context, c *app.RequestContext, next func()) {
	// 路径不匹配 → 跳过
	if string(c.Method()) != "GET" ||
		c.FullPath() != "/api/v1/buckets/:bucket_name/objects/:object_key" {
		next()
		return
	}

	// token 参数不存在 → 跳过（允许后续 Handler 用签名方式认证）
	token := c.Query("token")
	if token == "" {
		next()
		return
	}

	ctrl := ossAuth.NewTokenCtrl(h.adaptor)
	ak, pass := ctrl.ValidateToken(ctx, token, consts.DownloadAction,
		c.Param("bucket_name"), c.Param("object_key"))
	if !pass {
		c.JSON(401, common.AuthErr.WithMsg("invalid token"))
		c.Abort()
		return
	}

	info, sk, err := lookupAndDecryptSK(ctx, h.repo, h.adaptor.GetConfig().Security.AESKey, ak)
	if err != nil {
		c.JSON(500, common.ServerErr)
		c.Abort()
		return
	}

	setUserContext(ctx, c, info, sk)
	c.Set(consts.TokenGranted, true)
	c.Next(ctx)
}

// ============================================================
// Handler 2：OSS Token Header（X-OSS-Token，分片上传等）
// ============================================================

// ossTokenHandler 处理通过 X-OSS-Token Header 传入临时令牌的请求。
// 仅对 needTokenCheck 白名单中的路径生效。
type ossTokenHandler struct {
	repo    akRepo.IAccessKeyRepo
	adaptor adaptor.IAdaptor
}

func (h *ossTokenHandler) Handle(ctx context.Context, c *app.RequestContext, next func()) {
	ossToken := string(c.GetHeader(consts.HeaderToken))
	if ossToken == "" {
		next()
		return
	}

	curPath := string(c.Method()) + " " + c.FullPath()
	action, ok := needTokenCheck[curPath]
	if !ok {
		// 有 Token Header 但路径不在白名单：继续向下，交给签名校验
		next()
		return
	}

	ctrl := ossAuth.NewTokenCtrl(h.adaptor)
	ak, pass := ctrl.ValidateToken(ctx, ossToken, action,
		c.Param("bucket_name"), c.Param("object_key"))
	if !pass {
		c.JSON(401, common.AuthErr.WithMsg("invalid token"))
		c.Abort()
		return
	}

	info, sk, err := lookupAndDecryptSK(ctx, h.repo, h.adaptor.GetConfig().Security.AESKey, ak)
	if err != nil {
		c.JSON(500, common.ServerErr)
		c.Abort()
		return
	}

	setUserContext(ctx, c, info, sk)
	c.Set(consts.TokenGranted, true)
	c.Next(ctx)
}

// ============================================================
// Handler 3：HMAC 签名（Authorization: OSS AK:timestamp:signature）
// ============================================================

// hmacSignatureHandler 处理标准的 OSS HMAC-SHA256 签名认证。
// 格式：Authorization: OSS <AccessKey>:<timestamp>:<signature>
type hmacSignatureHandler struct {
	repo    akRepo.IAccessKeyRepo
	adaptor adaptor.IAdaptor
}

func (h *hmacSignatureHandler) Handle(ctx context.Context, c *app.RequestContext, next func()) {
	authHeader := string(c.GetHeader("Authorization"))
	if authHeader == "" {
		next()
		return
	}

	// 格式校验：OSS AKID123456:1746000000:abc123...
	parts := strings.SplitN(authHeader, " ", 2)
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

	ak, timestampStr, signature := fields[0], fields[1], fields[2]

	timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil || timestamp <= 0 {
		api.WriteResp(c, nil, common.AuthErr.WithMsg("invalid timestamp"))
		c.Abort()
		return
	}

	// 防重放：时间戳偏差不超过 30s
	if math.Abs(float64(time.Now().Unix()-timestamp)) > 30 {
		c.JSON(401, common.AuthErr.WithMsg("request expired"))
		c.Abort()
		return
	}

	// 查 AK / 解密 SK（验签前必须先拿到 SK）
	info, sk, err := lookupAndDecryptSK(ctx, h.repo, h.adaptor.GetConfig().Security.AESKey, ak)
	if err != nil {
		c.JSON(401, common.AuthErr.WithMsg("invalid access key"))
		c.Abort()
		return
	}

	// 重建待签名字符串
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

	query := canonicalQuery(string(c.GetRequest().QueryString()))
	stringToSign := buildStringToSign(
		string(c.Method()),
		string(c.Path()),
		query,
		string(c.Host()),
		contentType,
		body,
		timestamp,
	)

	if !tools.HmacSHA256Verify(stringToSign, sk, signature) {
		c.JSON(401, common.AuthErr.WithMsg("invalid signature"))
		c.Abort()
		return
	}

	setUserContext(ctx, c, info, sk)
	c.Next(ctx)
}

// ============================================================
// Handler 3：HMAC 签名（Authorization: OSS AK:timestamp:signature）
// ============================================================

// hmacSignatureHandler 处理标准的 OSS HMAC-SHA256 签名认证。
// 格式：Authorization: OSS <AccessKey>:<timestamp>:<signature>
type playTokenHandler struct {
	playToken redis.IPlayToken
}

func (h *playTokenHandler) Handle(ctx context.Context, c *app.RequestContext, next func()) {
	if !isVideoPlaybackPath(c) {
		next()
		return
	}

	token := extractPlayToken(c)
	if token == "" {
		api.WriteResp(c, nil, common.AuthErr.WithMsg("token is empty"))
		c.Abort()
		return
	}

	claims, err := h.playToken.GetPlayToken(ctx, token)
	if err != nil {
		if errors.Is(err, redis.ErrTokenNotFound) {
			api.WriteResp(c, nil, common.TokenExpired.WithMsg("play token expired or not found"))
		} else {
			api.WriteResp(c, nil, common.RedisErr.WithMsg("failed to validate play token"))
		}
		c.Abort()
		return
	}

	if claims == nil {
		api.WriteResp(c, nil, common.TokenInvalid.WithMsg("play token is invalid"))
		c.Abort()
		return
	}

	if claims.Action != consts.PlayVideoAction {
		api.WriteResp(c, nil, common.PermissionErr.WithMsg("invalid play token action"))
		c.Abort()
		return
	}

	if claims.ExpiresAt <= 0 || time.Now().Unix() > claims.ExpiresAt {
		api.WriteResp(c, nil, common.TokenExpired.WithMsg("play token expired"))
		c.Abort()
		return
	}

	if claims.UserID <= 0 ||
		claims.BucketID <= 0 ||
		strings.TrimSpace(claims.BucketName) == "" ||
		claims.ObjectID <= 0 ||
		strings.TrimSpace(claims.ObjectKey) == "" ||
		strings.TrimSpace(claims.VersionID) == "" ||
		claims.TranscodeID <= 0 {
		api.WriteResp(c, nil, common.TokenInvalid.WithMsg("play token missing required binding fields"))
		c.Abort()
		return
	}

	tempClient := &common.VideoPlayTokenCtx{
		Token:       token,
		UserID:      claims.UserID,
		BucketID:    claims.BucketID,
		BucketName:  claims.BucketName,
		ObjectID:    claims.ObjectID,
		ObjectKey:   claims.ObjectKey,
		VersionID:   claims.VersionID,
		TranscodeID: claims.TranscodeID,
		ExpiresAt:   claims.ExpiresAt,
		Action:      claims.Action,
	}

	c.Set(common.PlayTokenClaimsKey, tempClient)
	c.Next(ctx)
}

func isVideoPlaybackPath(c *app.RequestContext) bool {
	fullPath := c.FullPath()
	return fullPath == "/api/v1/video/keys/:key_id" ||
		fullPath == "/api/v1/video/hls/:transcode_id/master.m3u8" ||
		fullPath == "/api/v1/video/hls/:transcode_id/:profile/index.m3u8" ||
		fullPath == "/api/v1/video/hls/:transcode_id/:profile/:segment"
}

func extractPlayToken(c *app.RequestContext) string {
	// 优先从 header 取
	if token := strings.TrimSpace(string(c.GetHeader(consts.HeaderPlayToken))); token != "" {
		return token
	}
	// 其次从 query 取
	return strings.TrimSpace(c.Query("token"))
}

// ============================================================
// Handler 4：兜底 Fallback（链尾，所有方式均未匹配）
// ============================================================

// fallbackHandler 是责任链的最后一个节点。
// 走到这里说明没有任何认证方式匹配，直接返回 401。
type fallbackHandler struct{}

func (h *fallbackHandler) Handle(ctx context.Context, c *app.RequestContext, _ func()) {
	c.JSON(401, common.AuthErr)
	c.Abort()
}

// ============================================================
// 链式组装
// ============================================================
type chainRunner struct {
	handlers []authHandler
	index    int
}

func (r *chainRunner) next(ctx context.Context, c *app.RequestContext) {
	if r.index >= len(r.handlers) {
		(&fallbackHandler{}).Handle(ctx, c, nil)
		return
	}
	h := r.handlers[r.index]
	r.index++ // 先移动游标，再执行，防止 handler 内重复调用 next 死循环
	h.Handle(ctx, c, func() {
		r.next(ctx, c)
	})
}

// buildChain 将 handlers 列表递归地组装成一条责任链，末尾自动追加 fallbackHandler。
// 每个节点的 next() 就是对下一个节点的 Handle 调用。
func buildChain(handlers []authHandler) func(context.Context, *app.RequestContext) {
	return func(ctx context.Context, c *app.RequestContext) {
		runner := &chainRunner{handlers: handlers}
		runner.next(ctx, c)
	}
}

// ============================================================
// 对外入口（路由层零侵入，签名与原来完全一致）
// ============================================================

// NewAccessKeyMiddleware 构建认证中间件，内部使用责任链模式。
//
// 认证顺序：
//  1. 下载 Token（?token= 查询参数）
//  2. OSS Token Header（X-OSS-Token，分片上传白名单路径）
//  3. HMAC 签名（Authorization: OSS AK:timestamp:signature）
//  4. 兜底 → 401
//
// 新增认证方式：实现 authHandler 接口，在 buildChain 列表中追加即可，
// 无需修改任何现有逻辑。
func NewAccessKeyMiddleware(adaptor adaptor.IAdaptor) app.HandlerFunc {
	repo := akGorm.NewAccessKeyRepo(adaptor)

	chain := buildChain([]authHandler{
		&downloadTokenHandler{repo: repo, adaptor: adaptor},
		&ossTokenHandler{repo: repo, adaptor: adaptor},
		&hmacSignatureHandler{repo: repo, adaptor: adaptor},

		// 未来新增认证方式：在此追加一行，其余代码不动
	})

	return func(ctx context.Context, c *app.RequestContext) {
		if string(c.Method()) == "OPTIONS" {
			c.Next(ctx)
			return
		}
		chain(ctx, c)
	}
}
