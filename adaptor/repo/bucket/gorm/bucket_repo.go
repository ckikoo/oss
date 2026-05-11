package gorm

import (
	"context"
	"encoding/json"
	"time"

	"oss/adaptor"
	"oss/adaptor/repo/bucket"
	"oss/adaptor/repo/model"
	"oss/adaptor/repo/query"
	"oss/adaptor/repo/repoerr"
	"oss/adaptor/tx"
	"oss/consts"
	"oss/service/do"
	"oss/utils/logger"

	"github.com/go-redis/redis/v8"
	"github.com/gogf/gf/util/gconv"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type BucketRepo struct {
	db  *gorm.DB
	rds *redis.Client
}

var _ bucket.IBucketRepo = (*BucketRepo)(nil)

func NewBucketRepo(a adaptor.IAdaptor) *BucketRepo {
	return &BucketRepo{
		db:  a.GetGORM(),
		rds: a.GetRedis(),
	}
}

func (r *BucketRepo) WithTx(tx tx.Tx) bucket.IBucketRepo {
	return &BucketRepo{
		db:  tx.(*gorm.DB),
		rds: r.rds, // Reuse redis client in tx context
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
// getCachedBucket retrieves bucket from cache, returns nil if not found
func (r *BucketRepo) getCachedBucket(ctx context.Context, key string) *do.BucketDo {
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
func (r *BucketRepo) setCachedBucket(ctx context.Context, key string, bucket *do.BucketDo) error {
	data, err := json.Marshal(bucket)
	if err != nil {
		return repoerr.Wrap(err)
	}
	return repoerr.Wrap(r.rds.Set(ctx, key, data, time.Duration(consts.CacheTTLBucket)*time.Second).Err())
}

// invalidateBucketCache removes related cache entries
func (r *BucketRepo) invalidateBucketCache(ctx context.Context, userID, bucketID int64, bucketName string) {
	keys := []string{
		consts.BucketCacheKeyByName(userID, bucketName),
	}

	keys = append(keys, consts.BucketCacheKeyByID(bucketID))
	r.rds.Del(ctx, keys...)
}
func (r *BucketRepo) CreateBucket(ctx context.Context, bucket *do.CreateBucket) (int64, error) {
	var err error

	q := query.Use(r.db).Bucket

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
	// Try cache first
	cacheKey := consts.BucketCacheKeyByName(userID, name)
	if cached := r.getCachedBucket(ctx, cacheKey); cached != nil {
		return cached, nil
	}

	// Cache miss, query database
	q := query.Use(r.db)
	modelBucket, err := q.Bucket.WithContext(ctx).Where(q.Bucket.UserID.Eq(userID), q.Bucket.Name.Eq(name)).First()
	if err != nil {
		return nil, repoerr.Wrap(err)
	}

	bucket := r.toBucketDo(modelBucket)

	// Cache the result
	if err := r.setCachedBucket(ctx, cacheKey, bucket); err != nil {
		logger.Warn("failed to set bucket cache", zap.Error(err), zap.String("key", cacheKey), zap.String("bucket", gconv.String(bucket)))
	}

	return bucket, nil
}

func (r *BucketRepo) GetByID(ctx context.Context, id int64) (*do.BucketDo, error) {
	// Try cache first
	cacheKey := consts.BucketCacheKeyByID(id)
	if cached := r.getCachedBucket(ctx, cacheKey); cached != nil {
		return cached, nil
	}

	// Cache miss, query database
	q := query.Use(r.db)
	modelBucket, err := q.Bucket.WithContext(ctx).Where(q.Bucket.ID.Eq(id)).First()
	if err != nil {
		return nil, repoerr.Wrap(err)
	}

	bucket := r.toBucketDo(modelBucket)

	// Cache the result
	if err := r.setCachedBucket(ctx, cacheKey, bucket); err != nil {
		logger.Warn("failed to set bucket cache", zap.Error(err), zap.String("key", cacheKey), zap.String("bucket", gconv.String(bucket)))
	}

	return bucket, nil
}

func (r *BucketRepo) ListByFilter(ctx context.Context, userID int64, status int32) ([]*do.BucketDo, error) {
	// Cache miss or filtered query, query database
	q := query.Use(r.db)
	qs := q.Bucket.WithContext(ctx).Where(q.Bucket.UserID.Eq(userID))
	if status > 0 {
		qs = qs.Where(q.Bucket.Status.Eq(status))
	} else {
		qs = qs.Where(q.Bucket.Status.Neq(consts.BucketStatusDeleted))
	}

	modelBuckets, err := qs.Order(q.Bucket.ID.Desc()).Find()
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
	qs := query.Use(r.db).Bucket

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
		return nil, gorm.ErrInvalidData
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
	q := query.Use(r.db)
	_, err := q.Bucket.WithContext(ctx).Where(q.Bucket.UserID.Eq(userID), q.Bucket.Name.Eq(name)).Update(q.Bucket.Status, consts.BucketStatusDeleted)

	// Invalidate cache after delete
	r.invalidateBucketCache(ctx, userID, id, name) // bucketID unknown, but list cache will be cleared

	return repoerr.Wrap(err)
}

func (r *BucketRepo) UpdateBucketStats(ctx context.Context, userID int64, bucketName string, deltaCount, deltaSize int64) error {
	bucketInfo, err := r.GetByUserAndName(ctx, userID, bucketName)
	if err != nil {
		return repoerr.Wrap(err)
	}

	q := query.Use(r.db)
	_, err = q.Bucket.WithContext(ctx).
		Where(q.Bucket.UserID.Eq(userID), q.Bucket.Name.Eq(bucketName)).
		Updates(map[string]interface{}{
			q.Bucket.ObjectCount.ColumnName().String(): q.Bucket.ObjectCount.Add(deltaCount),
			q.Bucket.StorageSize.ColumnName().String(): q.Bucket.StorageSize.Add(deltaSize),
		})

	r.invalidateBucketCache(ctx, userID, bucketInfo.ID, bucketName)

	return repoerr.Wrap(err)
}
