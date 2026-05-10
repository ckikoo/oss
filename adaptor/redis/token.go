package redis

import (
	"context"
	"errors"
	"fmt"
	"oss/adaptor"
	"oss/consts"
	"oss/service/dto"
	"strconv"
	"time"

	"github.com/go-redis/redis/v8"
)

// ── 导出字段常量，调用方用常量而非裸字符串 ──────────────────────────────────

const (
	// 上传 / 下载 token 共用字段
	FieldBucketName = "bucket_name"
	FieldObjectKey  = "object_key"
	FieldExpiresIn  = "expires_in"

	// 仅上传 token
	FieldUserID      = "user_id"
	FieldMimeLimit   = "mime_limit"
	FieldSizeLimit   = "size_limit"
	FieldOverwrite   = "overwrite"
	FieldCallbackURL = "callback_url"
)

// ErrTokenNotFound token 不存在或已过期
var ErrTokenNotFound = errors.New("token not found or expired")

// ── 接口定义 ─────────────────────────────────────────────────────────────────

type IToken interface {
	// CreateUploadToken 创建上传 token（Hash 结构）
	CreateUploadToken(ctx context.Context, token string, req *dto.CreateUploadTokenReq, expire time.Duration) error
	// CreateDownloadToken 创建下载 token（Hash 结构）
	CreateDownloadToken(ctx context.Context, token string, req *dto.CreateDownloadTokenReq, expire time.Duration) error

	// GetUploadToken 读取上传 token 完整信息（HGETALL）
	GetUploadToken(ctx context.Context, token string) (*dto.CreateUploadTokenReq, error)
	// GetDownloadToken 读取下载 token 完整信息（HGETALL）
	GetDownloadToken(ctx context.Context, token string) (*dto.CreateDownloadTokenReq, error)

	// GetUploadTokenFields 读取上传 token 的指定字段
	// 单字段 → HGET，多字段 → HMGET，均为一次 RTT
	//
	//   vals, err := rds.GetUploadTokenFields(ctx, token, redis.FieldBucketName, redis.FieldSizeLimit)
	//   bucketName := vals[redis.FieldBucketName]
	GetUploadTokenFields(ctx context.Context, token string, fields ...string) (map[string]string, error)

	// GetDownloadTokenFields 读取下载 token 的指定字段
	//
	//   vals, err := rds.GetDownloadTokenFields(ctx, token, redis.FieldObjectKey)
	GetDownloadTokenFields(ctx context.Context, token string, fields ...string) (map[string]string, error)

	// DeleteUploadToken / DeleteDownloadToken 主动吊销（单次 token 用完即删）
	DeleteUploadToken(ctx context.Context, token string) error
	DeleteDownloadToken(ctx context.Context, token string) error
}

// ── 实现 ──────────────────────────────────────────────────────────────────────

type Token struct {
	rds *redis.Client
}

func NewToken(adaptor adaptor.IAdaptor) *Token {
	return &Token{rds: adaptor.GetRedis()}
}

var _ IToken = (*Token)(nil)

func uploadTokenKey(token string) string {
	return fmt.Sprintf("%s:upload:%s", consts.ServerName, token)
}

func downloadTokenKey(token string) string {
	return fmt.Sprintf("%s:download:%s", consts.ServerName, token)
}

// ── Upload Token ──────────────────────────────────────────────────────────────

func (t *Token) CreateUploadToken(ctx context.Context, token string, req *dto.CreateUploadTokenReq, expire time.Duration) error {
	key := uploadTokenKey(token)
	_, err := t.rds.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.HMSet(ctx, key, uploadTokenFields(req))
		pipe.Expire(ctx, key, expire)
		return nil
	})
	return err
}

func (t *Token) GetUploadToken(ctx context.Context, token string) (*dto.CreateUploadTokenReq, error) {
	vals, err := t.rds.HGetAll(ctx, uploadTokenKey(token)).Result()
	if err != nil {
		return nil, err
	}
	if len(vals) == 0 {
		return nil, ErrTokenNotFound
	}
	return parseUploadToken(vals)
}

func (t *Token) GetUploadTokenFields(ctx context.Context, token string, fields ...string) (map[string]string, error) {
	return hgetFields(ctx, t.rds, uploadTokenKey(token), fields...)
}

func (t *Token) DeleteUploadToken(ctx context.Context, token string) error {
	return t.rds.Del(ctx, uploadTokenKey(token)).Err()
}

// ── Download Token ────────────────────────────────────────────────────────────

func (t *Token) CreateDownloadToken(ctx context.Context, token string, req *dto.CreateDownloadTokenReq, expire time.Duration) error {
	key := downloadTokenKey(token)
	_, err := t.rds.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.HMSet(ctx, key, downloadTokenFields(req))
		pipe.Expire(ctx, key, expire)
		return nil
	})
	return err
}

func (t *Token) GetDownloadToken(ctx context.Context, token string) (*dto.CreateDownloadTokenReq, error) {
	vals, err := t.rds.HGetAll(ctx, downloadTokenKey(token)).Result()
	if err != nil {
		return nil, err
	}
	if len(vals) == 0 {
		return nil, ErrTokenNotFound
	}
	return parseDownloadToken(vals)
}

func (t *Token) GetDownloadTokenFields(ctx context.Context, token string, fields ...string) (map[string]string, error) {
	return hgetFields(ctx, t.rds, downloadTokenKey(token), fields...)
}

func (t *Token) DeleteDownloadToken(ctx context.Context, token string) error {
	return t.rds.Del(ctx, downloadTokenKey(token)).Err()
}

// ── 核心：字段查询（单/多字段统一入口） ──────────────────────────────────────

// hgetFields 是 GetXxxTokenFields 的底层实现。
//
// 路由逻辑：
//   - 1 个字段 → HGET（语义更清晰）
//   - N 个字段 → HMGET（单次 RTT，返回结果按请求顺序对应）
//
// 返回规则：
//   - key 不存在（token 过期）→ ErrTokenNotFound
//   - key 存在但某字段不存在 → 该字段不出现在返回 map 中（非 error）
//   - 调用方可通过 ok-idiom 判断字段是否存在：val, ok := result[FieldXxx]
func hgetFields(ctx context.Context, rds *redis.Client, key string, fields ...string) (map[string]string, error) {
	if len(fields) == 0 {
		return nil, errors.New("at least one field is required")
	}

	result := make(map[string]string, len(fields))

	if len(fields) == 1 {
		val, err := rds.HGet(ctx, key, fields[0]).Result()
		if err == redis.Nil {
			// 区分 key 不存在 vs 字段不存在
			if n, _ := rds.Exists(ctx, key).Result(); n == 0 {
				return nil, ErrTokenNotFound
			}
			return result, nil // key 存在，字段缺失，返回空 map
		}
		if err != nil {
			return nil, err
		}
		result[fields[0]] = val
		return result, nil
	}

	hasKey, err := rds.Exists(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	if hasKey == 0 {
		return nil, ErrTokenNotFound
	}

	// HMGET：不存在的 key 和不存在的字段都返回 nil
	vals, err := rds.HMGet(ctx, key, fields...).Result()
	if err != nil {
		return nil, err
	}

	for i, v := range vals {
		if v == nil {
			continue
		}
		if s, ok := v.(string); ok {
			result[fields[i]] = s
		}
	}

	return result, nil
}

// ── 序列化 / 反序列化 ─────────────────────────────────────────────────────────

func uploadTokenFields(req *dto.CreateUploadTokenReq) map[string]interface{} {
	return map[string]interface{}{
		FieldUserID:      req.UserId,
		FieldBucketName:  req.BucketName,
		FieldObjectKey:   req.ObjectKey,
		FieldExpiresIn:   req.ExpiresIn,
		FieldMimeLimit:   req.MimeLimit,
		FieldSizeLimit:   req.SizeLimit,
		FieldOverwrite:   boolToStr(req.Overwrite),
		FieldCallbackURL: req.CallbackUrl,
	}
}

func downloadTokenFields(req *dto.CreateDownloadTokenReq) map[string]interface{} {
	return map[string]interface{}{
		FieldBucketName: req.BucketName,
		FieldObjectKey:  req.ObjectKey,
		FieldExpiresIn:  req.ExpiresIn,
	}
}

func parseUploadToken(vals map[string]string) (*dto.CreateUploadTokenReq, error) {
	req := &dto.CreateUploadTokenReq{
		BucketName:  vals[FieldBucketName],
		ObjectKey:   vals[FieldObjectKey],
		MimeLimit:   vals[FieldMimeLimit],
		CallbackUrl: vals[FieldCallbackURL],
		Overwrite:   vals[FieldOverwrite] == "1",
	}
	if v, err := strconv.ParseInt(vals[FieldUserID], 10, 64); err == nil {
		req.UserId = v
	}
	if v, err := strconv.ParseInt(vals[FieldExpiresIn], 10, 64); err == nil {
		req.ExpiresIn = v
	}
	if v, err := strconv.ParseInt(vals[FieldSizeLimit], 10, 64); err == nil {
		req.SizeLimit = v
	}
	return req, nil
}

func parseDownloadToken(vals map[string]string) (*dto.CreateDownloadTokenReq, error) {
	req := &dto.CreateDownloadTokenReq{
		BucketName: vals[FieldBucketName],
		ObjectKey:  vals[FieldObjectKey],
	}
	if v, err := strconv.ParseInt(vals[FieldExpiresIn], 10, 64); err == nil {
		req.ExpiresIn = v
	}
	return req, nil
}

func boolToStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
