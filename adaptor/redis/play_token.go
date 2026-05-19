package redis

import (
	"context"
	"fmt"
	"oss/adaptor"
	"oss/consts"
	"oss/service/dto"
	"strconv"
	"time"

	"github.com/go-redis/redis/v8"
)

const (
	FieldAction      = "action"
	FieldBucketID    = "bucket_id"
	FieldObjectID    = "object_id"
	FieldVersionID   = "version_id"
	FieldTranscodeID = "transcode_id"
	FieldExpiresAt   = "expires_at"
)

type IPlayToken interface {
	CreatePlayToken(ctx context.Context, token string, req *dto.VideoPlayToken, expire time.Duration) error
	GetPlayToken(ctx context.Context, token string) (*dto.VideoPlayToken, error)
	GetPlayTokenFields(ctx context.Context, token string, fields ...string) (map[string]string, error)
	DeletePlayToken(ctx context.Context, token string) error
}

type PlayToken struct {
	rds *redis.Client
}

var _ IPlayToken = (*PlayToken)(nil)

func NewPlayToken(adaptor adaptor.IAdaptor) *PlayToken {
	return &PlayToken{rds: adaptor.GetRedis()}
}

func playTokenKey(token string) string {
	return fmt.Sprintf("%s:video:play:%s", consts.ServerName, token)
}

func (t *PlayToken) CreatePlayToken(ctx context.Context, token string, req *dto.VideoPlayToken, expire time.Duration) error {
	key := playTokenKey(token)
	_, err := t.rds.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.HMSet(ctx, key, playTokenFields(req))
		pipe.Expire(ctx, key, expire)
		return nil
	})
	return err
}

func (t *PlayToken) GetPlayToken(ctx context.Context, token string) (*dto.VideoPlayToken, error) {
	vals, err := t.rds.HGetAll(ctx, playTokenKey(token)).Result()
	if err != nil {
		return nil, err
	}
	if len(vals) == 0 {
		return nil, ErrTokenNotFound
	}
	req := parsePlayToken(vals)
	req.Token = token
	return req, nil
}

func (t *PlayToken) GetPlayTokenFields(ctx context.Context, token string, fields ...string) (map[string]string, error) {
	return hgetFields(ctx, t.rds, playTokenKey(token), fields...)
}

func (t *PlayToken) DeletePlayToken(ctx context.Context, token string) error {
	return t.rds.Del(ctx, playTokenKey(token)).Err()
}

func playTokenFields(req *dto.VideoPlayToken) map[string]interface{} {
	return map[string]interface{}{
		FieldUserID:      req.UserID,
		FieldBucketID:    req.BucketID,
		FieldBucketName:  req.BucketName,
		FieldObjectID:    req.ObjectID,
		FieldObjectKey:   req.ObjectKey,
		FieldVersionID:   req.VersionID,
		FieldTranscodeID: req.TranscodeID,
		FieldExpiresAt:   req.ExpiresAt,
		FieldAction:      req.Action,
	}
}

func parsePlayToken(vals map[string]string) *dto.VideoPlayToken {
	return &dto.VideoPlayToken{
		UserID:      parseInt64(vals[FieldUserID]),
		BucketID:    parseInt64(vals[FieldBucketID]),
		BucketName:  vals[FieldBucketName],
		ObjectID:    parseInt64(vals[FieldObjectID]),
		ObjectKey:   vals[FieldObjectKey],
		VersionID:   vals[FieldVersionID],
		TranscodeID: parseInt64(vals[FieldTranscodeID]),
		ExpiresAt:   parseInt64(vals[FieldExpiresAt]),
		Action:      vals[FieldAction],
	}
}

func parseInt64(raw string) int64 {
	value, _ := strconv.ParseInt(raw, 10, 64)
	return value
}
