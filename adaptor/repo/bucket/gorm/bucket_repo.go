package gorm

import (
	"context"
	"encoding/json"
	"time"

	"golang.org/x/sync/singleflight"

	"oss/adaptor"
	"oss/adaptor/repo/bucket"
	"oss/adaptor/repo/model"
	"oss/adaptor/repo/query"
	"oss/adaptor/repo/repocache"
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

type BucketRepo struct {
	db           *gorm.DB
	q            *query.Query
	rds          *redis.Client
	cacheManager cache.IManager // 只是持有
	g            *singleflight.Group
	cacheEnabled bool
}

var _ bucket.IBucketRepo = (*BucketRepo)(nil)

func NewBucketRepo(a adaptor.IAdaptor) *BucketRepo {
	return &BucketRepo{
		q:            query.Use(a.GetGORM()),
		db:           a.GetGORM(),
		rds:          a.GetRedis(),
		g:            &singleflight.Group{},
		cacheManager: a.GetCache(),
		cacheEnabled: true,
	}
}

func (r *BucketRepo) WithTx(tx tx.Tx) bucket.IBucketRepo {
	db := tx.(*gorm.DB)
	return &BucketRepo{
		q:            query.Use(db),
		db:           db,
		rds:          r.rds,
		g:            r.g,
		cacheManager: r.cacheManager,
		cacheEnabled: false,
	}
}

func (r *BucketRepo) toBucketDo(modelBucket *model.Bucket) *do.BucketDo {
	return &do.BucketDo{
		ID:           modelBucket.ID,
		UserID:       modelBucket.UserID,
		Name:         modelBucket.Name,
		Region:       modelBucket.Region,
		Acl:          modelBucket.Acl,
		Versioning:   modelBucket.Versioning,
		Status:       modelBucket.Status,
		StorageClass: modelBucket.StorageClass,
		ObjectCount:  modelBucket.ObjectCount,
		StorageSize:  modelBucket.StorageSize,
		CreatedAt:    modelBucket.CreatedAt,
		UpdatedAt:    modelBucket.UpdatedAt,
	}
}

// ─── Cache Helpers ────────────────────────────────────────────────────
// getCachedRedis retrieves bucket from Redis cache, returns nil if not found
func (r *BucketRepo) getCachedRedis(ctx context.Context, key string) *do.BucketDo {
	if r.rds == nil {
		return nil
	}
	val, err := r.rds.Get(ctx, key).Result()
	if err != nil {
		return nil
	}
	var bucket do.BucketDo
	if err := json.Unmarshal([]byte(val), &bucket); err != nil {
		// Cache corrupted, ignore
		return nil
	}
	return &bucket
}

// setCachedBucket stores bucket in cache with TTL
func (r *BucketRepo) setCachedRedis(ctx context.Context, key string, bucket *do.BucketDo) error {
	data, err := json.Marshal(bucket)
	if err != nil {
		return repoerr.Wrap(err)
	}
	return repoerr.Wrap(r.rds.Set(ctx, key, data, time.Duration(consts.CacheTTLBucket)*time.Second).Err())
}

// setAllCaches 本地 + Redis 同时写入
func (r *BucketRepo) setAllCaches(ctx context.Context, keys []string, b *do.BucketDo) {
	if r.cacheManager == nil {
		return
	}
	for _, key := range keys {
		r.cacheManager.Set(key, b, 0) // TTL=0 使用 manager 默认值

		if err := r.setCachedRedis(ctx, key, b); err != nil {
			logger.Warn("failed to set bucket redis cache",
				zap.Error(err),
				zap.String("key", key),
				zap.String("bucket", gconv.String(b)),
			)
		}
	}
}

// invalidateBucketCache 删本地 + 删 Redis + 广播其他实例
func (r *BucketRepo) invalidateBucketCache(ctx context.Context, userID, bucketID int64, bucketName string) {
	keys := []string{
		consts.BucketCacheKeyByName(userID, bucketName),
		consts.BucketCacheKeyByID(bucketID),
	}
	repocache.Invalidator{
		RDS:          r.rds,
		Local:        r.cacheManager,
		DoubleDelete: true,
		LogName:      "bucket",
	}.AfterCommit(ctx, keys...)
}

// ─── 三层查询核心 ────────────────────────────────────────────────────────────

// getByKey 统一的三层缓存查询：本地 → Redis → singleflight → DB
func (r *BucketRepo) getByKey(
	ctx context.Context,
	cacheKey string,
	queryFn func() (*do.BucketDo, error),
) (*do.BucketDo, error) {
	return repocache.Accessor[*do.BucketDo]{
		RDS:     r.rds,
		Local:   r.cacheManager,
		Group:   r.g,
		TTL:     time.Duration(consts.CacheTTLBucket) * time.Second,
		Enabled: r.cacheEnabled,
		LogName: "bucket",
	}.Get(ctx, cacheKey, func(context.Context) (*do.BucketDo, error) {
		return queryFn()
	})
}

// REPO
func (r *BucketRepo) CreateBucket(ctx context.Context, bucket *do.CreateBucket) (int64, error) {
	var err error

	q := r.q.Bucket

	modelBucket := &model.Bucket{
		UserID:       bucket.UserID,
		Name:         bucket.Name,
		Region:       bucket.Region,
		Acl:          bucket.Acl,
		Versioning:   bucket.Versioning,
		Status:       consts.BucketStatusNormal,
		StorageClass: bucket.StorageClass,
		ObjectCount:  0,
		StorageSize:  0,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err = q.WithContext(ctx).Save(modelBucket); err != nil {
		return 0, repoerr.Wrap(err)
	}

	return modelBucket.ID, nil
}

func (r *BucketRepo) GetByName(ctx context.Context, userID int64, name string) (*do.BucketDo, error) {
	return r.GetByUserAndName(ctx, userID, name)
}

func (r *BucketRepo) GetByUserAndName(ctx context.Context, userID int64, name string) (*do.BucketDo, error) {

	cacheKey := consts.BucketCacheKeyByName(userID, name)

	return r.getByKey(ctx, cacheKey, func() (*do.BucketDo, error) {
		// Cache miss, query database
		q := r.q.Bucket
		modelBucket, err := q.WithContext(ctx).Where(q.UserID.Eq(userID), q.Name.Eq(name)).First()
		if err != nil {
			return nil, repoerr.Wrap(err)
		}

		return r.toBucketDo(modelBucket), nil
	})
}

func (r *BucketRepo) GetByID(ctx context.Context, id int64) (*do.BucketDo, error) {
	// Try cache first
	cacheKey := consts.BucketCacheKeyByID(id)

	return r.getByKey(ctx, cacheKey, func() (*do.BucketDo, error) {
		// Cache miss, query database
		q := r.q.Bucket
		modelBucket, err := q.WithContext(ctx).Where(q.ID.Eq(id)).First()
		if err != nil {
			return nil, repoerr.Wrap(err)
		}

		return r.toBucketDo(modelBucket), nil
	})

}

func (r *BucketRepo) ListByFilter(ctx context.Context, userID int64, status int32) ([]*do.BucketDo, error) {
	// Cache miss or filtered query, query database
	q := r.q.Bucket
	qs := q.WithContext(ctx).Where(q.UserID.Eq(userID))
	if status > 0 {
		qs = qs.Where(q.Status.Eq(status))
	} else {
		qs = qs.Where(q.Status.Neq(consts.BucketStatusDeleted))
	}

	modelBuckets, err := qs.Order(q.ID.Desc()).Find()
	if err != nil {
		return nil, repoerr.Wrap(err)
	}

	buckets := make([]*do.BucketDo, len(modelBuckets))
	for i, modelBucket := range modelBuckets {
		buckets[i] = r.toBucketDo(modelBucket)
	}

	return buckets, nil
}

func (r *BucketRepo) UpdateBucket(ctx context.Context, userID int64, id int64, name string, update *do.UpdateBucket) (*do.BucketDo, error) {
	qs := r.q.Bucket

	updates := map[string]interface{}{}
	if update.Acl != nil {
		updates[qs.Acl.ColumnName().String()] = *update.Acl
	}
	if update.Versioning != nil {
		updates[qs.Versioning.ColumnName().String()] = *update.Versioning
	}
	if update.Status != nil {
		updates[qs.Status.ColumnName().String()] = *update.Status
	}
	if update.StorageClass != "" {
		updates[qs.StorageClass.ColumnName().String()] = update.StorageClass
	}
	if len(updates) == 0 {
		return nil, repoerr.Wrap(gorm.ErrInvalidData) // No fields to update
	}

	updates[qs.UpdatedAt.ColumnName().String()] = time.Now()

	if _, err := qs.WithContext(ctx).Where(qs.UserID.Eq(userID), qs.Name.Eq(name)).Updates(updates); err != nil {
		return nil, repoerr.Wrap(err)
	}

	r.invalidateBucketCache(ctx, userID, id, name)

	bucket, err := r.GetByUserAndName(ctx, userID, name)
	if err != nil {
		return nil, repoerr.Wrap(err)
	}

	return bucket, nil
}

func (r *BucketRepo) DeleteBucket(ctx context.Context, userID int64, id int64, name string) error {
	q := r.q.Bucket
	_, err := q.WithContext(ctx).Where(q.UserID.Eq(userID), q.Name.Eq(name)).Update(q.Status, consts.BucketStatusDeleted)
	if err != nil {
		return repoerr.Wrap(err) // ← 失败直接返回，不失效缓存
	}
	// Invalidate cache after delete
	r.invalidateBucketCache(ctx, userID, id, name)

	return nil
}

func (r *BucketRepo) UpdateBucketStats(ctx context.Context, userID int64, bucketName string, deltaCount, deltaSize int64) error {
	bucketInfo, err := r.GetByUserAndName(ctx, userID, bucketName)
	if err != nil {
		return repoerr.Wrap(err)
	}

	q := r.q.Bucket

	updateMap := map[string]interface{}{}

	if deltaCount != 0 {
		updateMap[q.ObjectCount.ColumnName().String()] = q.ObjectCount.Add(deltaCount)
	}
	if deltaSize != 0 {
		updateMap[q.StorageSize.ColumnName().String()] = q.StorageSize.Add(deltaSize)
	}

	if len(updateMap) == 0 {
		return nil
	}

	_, err = q.WithContext(ctx).
		Where(q.UserID.Eq(userID), q.Name.Eq(bucketName)).
		Updates(updateMap)
	if err != nil {
		return repoerr.Wrap(err)
	}
	r.invalidateBucketCache(ctx, userID, bucketInfo.ID, bucketName)
	return nil
}
