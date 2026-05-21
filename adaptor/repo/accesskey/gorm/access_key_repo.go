package gorm

import (
	"context"
	"encoding/json"
	"time"

	"golang.org/x/sync/singleflight"

	"oss/adaptor"
	"oss/adaptor/repo/accesskey"
	"oss/adaptor/repo/model"
	"oss/adaptor/repo/query"
	"oss/adaptor/repo/repoerr"
	"oss/adaptor/tx"
	"oss/consts"
	"oss/service/do"
	"oss/utils/cache"
	"oss/utils/logger"

	"github.com/go-redis/redis/v8"
	"github.com/gogf/gf/util/gconv"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type AccessKeyRepo struct {
	db           *gorm.DB
	q            *query.Query
	rds          *redis.Client
	cacheManager cache.IManager
	g            *singleflight.Group
}

var _ accesskey.IAccessKeyRepo = (*AccessKeyRepo)(nil)

func NewAccessKeyRepo(a adaptor.IAdaptor) *AccessKeyRepo {
	return &AccessKeyRepo{
		db:           a.GetGORM(),
		q:            query.Use(a.GetGORM()),
		rds:          a.GetRedis(),
		g:            &singleflight.Group{},
		cacheManager: a.GetCache(),
	}
}

func (r *AccessKeyRepo) WithTx(tx tx.Tx) accesskey.IAccessKeyRepo {
	txDB, _ := tx.(*gorm.DB)
	return &AccessKeyRepo{
		db:           txDB,
		q:            query.Use(txDB),
		rds:          r.rds,
		g:            r.g,
		cacheManager: r.cacheManager,
	}
}

func (r *AccessKeyRepo) toAccessKeyDo(modelAK *model.AccessKey) *do.AccessKeyDo {
	return &do.AccessKeyDo{
		ID:        modelAK.ID,
		UserID:    modelAK.UserID,
		AccessKey: modelAK.AccessKey,
		SecretKey: modelAK.SecretKey,
		Alias: func() string {
			if modelAK.Alias_ != nil {
				return *modelAK.Alias_
			}
			return ""
		}(),
		Status: modelAK.Status,
		Permission: func() string {
			if modelAK.Permission != nil {
				return *modelAK.Permission
			}
			return ""
		}(),
		CreatedAt: modelAK.CreatedAt.UnixMilli(),
		ExpiresAt: func() int64 {
			if modelAK.ExpiresAt != nil {
				return modelAK.ExpiresAt.UnixMilli()
			}
			return 0
		}(),
		LastUsedAt: func() int64 {
			if modelAK.LastUsedAt != nil {
				return modelAK.LastUsedAt.UnixMilli()
			}
			return 0
		}(),
	}
}

// ─── Cache Helpers ────────────────────────────────────────────────────
// getCachedRedis retrieves access key from Redis cache, returns nil if not found
func (r *AccessKeyRepo) getCachedRedis(ctx context.Context, key string) *do.AccessKeyDo {
	val, err := r.rds.Get(ctx, key).Result()
	if err != nil {
		return nil
	}
	var ak do.AccessKeyDo
	if err := json.Unmarshal([]byte(val), &ak); err != nil {
		// Cache corrupted, ignore
		return nil
	}
	return &ak
}

// setCachedAccessKey stores access key in cache with TTL
func (r *AccessKeyRepo) setCachedRedis(ctx context.Context, key string, ak *do.AccessKeyDo) error {
	data, err := json.Marshal(ak)
	if err != nil {
		return repoerr.Wrap(err)
	}
	return repoerr.Wrap(r.rds.Set(ctx, key, data, time.Duration(consts.CacheTTLAccessKey)*time.Second).Err())
}

// setAllCaches 本地 + Redis 同时写入
func (r *AccessKeyRepo) setAllCaches(ctx context.Context, keys []string, ak *do.AccessKeyDo) {
	for _, key := range keys {
		r.cacheManager.Set(key, ak, 0) // TTL=0 使用 manager 默认值

		if err := r.setCachedRedis(ctx, key, ak); err != nil {
			logger.Warn("failed to set access key redis cache",
				zap.Error(err),
				zap.String("key", key),
				zap.String("accessKey", gconv.String(ak)),
			)
		}
	}
}

// invalidateAccessKeyCache 删本地 + 删 Redis + 广播其他实例
func (r *AccessKeyRepo) invalidateAccessKeyCache(ctx context.Context, accessKey string) {
	keys := []string{
		consts.AccessKeyCacheKey(accessKey),
	}
	// 删 Redis
	r.rds.Del(ctx, keys...)

	// 删本地
	r.cacheManager.Remove(keys...)

	// 广播其他实例删本地
	if err := r.cacheManager.Publish(ctx, keys...); err != nil {
		logger.Warn("failed to publish access key cache invalidation",
			zap.Error(err),
			zap.Strings("keys", keys),
		)
	}
}

// ─── 三层查询核心 ────────────────────────────────────────────────────────────

// getByKey 统一的三层缓存查询：本地 → Redis → singleflight → DB
func (r *AccessKeyRepo) getByKey(
	ctx context.Context,
	cacheKey string,
	queryFn func() (*do.AccessKeyDo, error),
) (*do.AccessKeyDo, error) {

	// ① 本地缓存
	if entry, ok := r.cacheManager.Get(cacheKey); ok {
		return entry.Data.(*do.AccessKeyDo), nil
	}

	// ② Redis 缓存
	if cached := r.getCachedRedis(ctx, cacheKey); cached != nil {
		r.cacheManager.Set(cacheKey, cached, 0) // 回填本地
		return cached, nil
	}

	// ③ singleflight → DB（同实例并发合并，防击穿）
	result, err, _ := r.g.Do(cacheKey, func() (interface{}, error) {
		// double-check Redis（可能其他实例刚写入）
		if cached := r.getCachedRedis(ctx, cacheKey); cached != nil {
			return cached, nil
		}

		ak, err := queryFn()
		if err != nil {
			return nil, err
		}

		// 回填 Redis + 本地
		r.setAllCaches(ctx, []string{cacheKey}, ak)
		return ak, nil
	})

	if err != nil {
		return nil, err
	}

	ak := result.(*do.AccessKeyDo)

	// singleflight 共享结果时也要回填本地（其他等待的 goroutine 没有走 setAllCaches）
	r.cacheManager.Set(cacheKey, ak, 0)

	return ak, nil
}

func (r *AccessKeyRepo) CreateAccessKey(ctx context.Context, ak *do.CreateAccessKey) (int64, error) {
	modelAK := &model.AccessKey{
		UserID:     ak.UserID,
		AccessKey:  ak.AccessKey,
		SecretKey:  ak.SecretKey,
		Permission: ak.Permission,
		ExpiresAt:  ak.ExpiresAt,
		Status:     consts.AccessKeyStatusEnable,
		CreatedAt:  time.Now(),
	}
	qs := r.q.AccessKey.WithContext(ctx)
	err := qs.Create(modelAK)
	if err != nil {
		return 0, repoerr.Wrap(err)
	}
	return modelAK.ID, nil
}

func (r *AccessKeyRepo) GetByAccessKey(ctx context.Context, accessKey string) (*do.AccessKeyDo, error) {
	cacheKey := consts.AccessKeyCacheKey(accessKey)

	return r.getByKey(ctx, cacheKey, func() (*do.AccessKeyDo, error) {
		// Cache miss, query database
		q := r.q
		qs := q.AccessKey.WithContext(ctx)
		modelAK, err := qs.Where(q.AccessKey.AccessKey.Eq(accessKey)).First()
		if err != nil {
			return nil, repoerr.Wrap(err)
		}
		return r.toAccessKeyDo(modelAK), nil
	})
}

func (r *AccessKeyRepo) CheckAccessKeyAndSecret(ctx context.Context, accessKey string, secretKeyHash string) bool {
	qs := r.q.AccessKey

	count, _ := qs.WithContext(ctx).Where(qs.SecretKey.Eq(secretKeyHash), qs.AccessKey.Eq(accessKey)).Count()
	return count > 0
}

func (r *AccessKeyRepo) ListByFilter(ctx context.Context, userID int64, status int32) ([]*do.AccessKeyDo, error) {
	q := r.q
	qs := q.AccessKey.WithContext(ctx)
	if userID > 0 {
		qs = qs.Where(q.AccessKey.UserID.Eq(userID))
	}
	if status != 0 {
		qs = qs.Where(q.AccessKey.Status.Eq(status))
	}
	modelAKs, err := qs.Order(q.AccessKey.ID.Desc()).Find()
	if err != nil {
		return nil, repoerr.Wrap(err)
	}
	doAKs := make([]*do.AccessKeyDo, len(modelAKs))
	for i, modelAK := range modelAKs {
		doAKs[i] = r.toAccessKeyDo(modelAK)
	}
	return doAKs, nil
}

func (r *AccessKeyRepo) UpdateStatus(ctx context.Context, accessKey string, status int32) (*do.AccessKeyDo, error) {
	q := r.q
	qs := q.AccessKey.WithContext(ctx)
	_, err := qs.Where(q.AccessKey.AccessKey.Eq(accessKey)).Update(q.AccessKey.Status, status)
	if err != nil {
		return nil, repoerr.Wrap(err)
	}

	// Invalidate cache
	r.invalidateAccessKeyCache(ctx, accessKey)

	return r.GetByAccessKey(ctx, accessKey)
}

func (r *AccessKeyRepo) DeleteAccessKey(ctx context.Context, accessKey string) error {
	q := r.q
	qs := q.AccessKey.WithContext(ctx)
	_, err := qs.Where(q.AccessKey.AccessKey.Eq(accessKey)).Delete()
	if err != nil {
		return repoerr.Wrap(err)
	}

	// Invalidate cache
	r.invalidateAccessKeyCache(ctx, accessKey)

	return nil
}
