package gorm

import (
	"context"
	"encoding/json"
	"time"

	"oss/adaptor"
	"oss/adaptor/repo/model"
	"oss/adaptor/repo/object"
	"oss/adaptor/repo/query"
	"oss/adaptor/repo/repoerr"
	"oss/adaptor/tx"
	"oss/consts"
	"oss/service/do"
	"oss/utils/logger"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type objectRepo struct {
	db  *gorm.DB
	rds *redis.Client
}

var _ object.IObjectRepo = (*objectRepo)(nil)

func NewObjectRepo(a adaptor.IAdaptor) object.IObjectRepo {
	return &objectRepo{
		db:  a.GetGORM(),
		rds: a.GetRedis(),
	}
}

func (r *objectRepo) WithTx(tx tx.Tx) object.IObjectRepo {
	return &objectRepo{
		db:  tx.(*gorm.DB),
		rds: r.rds,
	}
}
func (r *objectRepo) toObjectDo(modelObject *model.Object) *do.ObjectDo {
	return &do.ObjectDo{
		ID:            modelObject.ID,
		BucketID:      modelObject.BucketID,
		BucketName:    modelObject.BucketName,
		ObjectKey:     modelObject.ObjectKey,
		ObjectKeyHash: modelObject.ObjectKeyHash,
		VersionID:     modelObject.VersionID,
		Size:          modelObject.Size,
		Etag:          modelObject.Etag,
		ContentType:   modelObject.ContentType,
		StorageClass:  modelObject.StorageClass,
		IsMultipart:   modelObject.IsMultipart,
		UploadID:      modelObject.UploadID,
		StoragePath:   modelObject.StoragePath,
		Acl:           modelObject.Acl,
		Metadata:      modelObject.Metadata,
		Status:        modelObject.Status,
		AccessCount:   modelObject.AccessCount,
		CreatedAt:     modelObject.CreatedAt,
		UpdatedAt:     modelObject.UpdatedAt,
		DeletedAt:     modelObject.DeletedAt,
	}
}

// getCachedObject retrieves object from cache
func (r *objectRepo) getCachedObject(ctx context.Context, key string) *do.ObjectDo {
	val, err := r.rds.Get(ctx, key).Result()
	if err != nil {
		return nil
	}
	var object do.ObjectDo
	if err := json.Unmarshal([]byte(val), &object); err != nil {
		// Cache corrupted, ignore
		return nil
	}
	return &object
}

// setCachedObject stores object in cache with TTL
func (r *objectRepo) setCachedObject(ctx context.Context, key string, object *do.ObjectDo) error {
	data, err := json.Marshal(object)
	if err != nil {
		return repoerr.Wrap(err)
	}
	return repoerr.Wrap(r.rds.Set(ctx, key, data, time.Duration(consts.CacheTTLObject)*time.Second).Err())
}

// invalidateObjectCache removes related cache entries
func (r *objectRepo) invalidateObjectCache(ctx context.Context, bucketName, objectKey, versionID string) {
	key := consts.ObjectCacheKey(bucketName, objectKey, versionID)
	r.rds.Del(ctx, key)
}

// repoerr.Wrap maps GORM errors to repoerr sentinels

func (r *objectRepo) CreateObject(ctx context.Context, object *do.CreateObject) (int64, error) {

	var (
		objectID int64 = 0
		err      error
	)

	qsBucket := query.Use(r.db).Bucket

	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		modelObject := &model.Object{
			BucketID:      object.BucketID,
			BucketName:    object.BucketName,
			ObjectKey:     object.ObjectKey,
			ObjectKeyHash: object.ObjectKeyHash,
			VersionID:     object.VersionID,
			Size:          object.Size,
			Etag:          object.Etag,
			ContentType:   object.ContentType,
			StorageClass:  object.StorageClass,
			IsMultipart:   object.IsMultipart,
			UploadID:      object.UploadID,
			StoragePath:   object.StoragePath,
			Acl:           object.Acl,
			Metadata:      object.Metadata,
			Status:        consts.ObjectStatusNormal,
			AccessCount:   0,
			CreatedAt:     now,
			UpdatedAt:     now,
		}

		if err := tx.Create(modelObject).Error; err != nil {
			return repoerr.Wrap(err)
		}

		objectID = modelObject.ID

		result := tx.Model(&model.Bucket{}).Where(qsBucket.ID.Eq(object.BucketID)).Updates(map[string]interface{}{
			qsBucket.ObjectCount.ColumnName().String(): qsBucket.ObjectCount.Add(1),
			qsBucket.StorageSize.ColumnName().String(): qsBucket.StorageSize.Add(object.Size),
		})
		if result.Error != nil {
			return repoerr.Wrap(result.Error)
		}

		return object.CallBack(tx)

	})

	if err != nil {
		return 0, repoerr.Wrap(err)
	}

	return objectID, nil
}
func (r *objectRepo) GetObjectFromHashKey(ctx context.Context, req *do.GetObjectFromHashKey) (*do.ObjectDo, error) {
	q := query.Use(r.db)
	qs := q.Object.WithContext(ctx).Where(q.Object.BucketName.Eq(req.BucketName), q.Object.ObjectKeyHash.Eq(req.ObjectKeyHash))

	modelObject, err := qs.First()
	if err != nil {
		return nil, repoerr.Wrap(err)
	}
	return r.toObjectDo(modelObject), nil
}
func (r *objectRepo) GetByKey(ctx context.Context, bucketName, objectKey, versionID string) (*do.ObjectDo, error) {
	// Try cache first
	cacheKey := consts.ObjectCacheKey(bucketName, objectKey, versionID)
	if cached := r.getCachedObject(ctx, cacheKey); cached != nil {
		return cached, nil
	}

	q := query.Use(r.db)
	qs := q.Object.WithContext(ctx).Where(q.Object.BucketName.Eq(bucketName), q.Object.ObjectKey.Eq(objectKey))
	if versionID != "" {
		qs = qs.Where(q.Object.VersionID.Eq(versionID))
	} else {
		qs = qs.Where(q.Object.VersionID.Eq(""))
	}

	modelObject, err := qs.First()
	if err != nil {
		return nil, repoerr.Wrap(err)
	}

	objectDo := r.toObjectDo(modelObject)

	// Cache the result
	if err := r.setCachedObject(ctx, cacheKey, objectDo); err != nil {
		logger.GetLogger().Warn("Failed to cache object", zap.Error(err))
	}

	return objectDo, nil
}

func (r *objectRepo) ListByFilter(ctx context.Context, bucketName, prefix, delimiter, marker string, maxKeys int, versionID string) ([]*do.ObjectDo, error) {
	q := query.Use(r.db)
	qs := q.Object.WithContext(ctx).Where(q.Object.BucketName.Eq(bucketName), q.Object.Status.Neq(consts.ObjectStatusDeleted))

	if prefix != "" {
		qs = qs.Where(q.Object.ObjectKey.Like(prefix + "%"))
	}
	if marker != "" {
		qs = qs.Where(q.Object.ObjectKey.Gt(marker))
	}
	if versionID != "" {
		qs = qs.Where(q.Object.VersionID.Eq(versionID))
	} else {
		qs = qs.Where(q.Object.VersionID.Eq(""))
	}

	if maxKeys > 0 {
		qs = qs.Limit(maxKeys)
	} else {
		qs = qs.Limit(consts.DefaultMaxKeys)
	}

	modelObjects, err := qs.Order(q.Object.ObjectKey).Find()
	if err != nil {
		return nil, repoerr.Wrap(err)
	}

	objects := make([]*do.ObjectDo, len(modelObjects))
	for i, modelObject := range modelObjects {
		objects[i] = r.toObjectDo(modelObject)
	}
	return objects, nil
}

func (r *objectRepo) UpdateObject(ctx context.Context, bucketName, objectKey, versionID string, update *do.UpdateObject) (*do.ObjectDo, error) {
	// Invalidate cache before update
	r.invalidateObjectCache(ctx, bucketName, objectKey, versionID)

	q := query.Use(r.db).Object

	updates := map[string]interface{}{}
	if update.Size != nil {
		updates[q.Size.ColumnName().String()] = *update.Size
	}
	if update.Etag != nil {
		updates[q.Etag.ColumnName().String()] = *update.Etag
	}
	if update.ContentType != nil {
		updates[q.ContentType.ColumnName().String()] = *update.ContentType
	}
	if update.StorageClass != nil {
		updates[q.StorageClass.ColumnName().String()] = *update.StorageClass
	}
	if update.StoragePath != nil {
		updates[q.StoragePath.ColumnName().String()] = *update.StoragePath
	}
	if update.Acl != nil {
		updates[q.Acl.ColumnName().String()] = *update.Acl
	}
	if update.Metadata != nil {
		updates[q.Metadata.ColumnName().String()] = *update.Metadata
	}
	if update.Status != nil {
		updates[q.Status.ColumnName().String()] = *update.Status
	}

	if update.IsMultipart != nil {
		updates[q.IsMultipart.ColumnName().String()] = *update.IsMultipart
	}

	if len(updates) == 0 {
		return nil, gorm.ErrInvalidData
	}

	updates[q.UpdatedAt.ColumnName().String()] = time.Now()

	qs := q.WithContext(ctx).Where(q.BucketName.Eq(bucketName), q.ObjectKey.Eq(objectKey))
	if versionID != "" {
		qs = qs.Where(q.VersionID.Eq(versionID))
	} else {
		qs = qs.Where(q.VersionID.Eq(""))
	}

	_, err := qs.Updates(updates)
	if err != nil {
		return nil, repoerr.Wrap(err)
	}

	r.invalidateObjectCache(ctx, bucketName, objectKey, versionID)

	return r.GetByKey(ctx, bucketName, objectKey, versionID)
}

func (r *objectRepo) UpdateObjectStorageClass(ctx context.Context, bucketName, objectKey, storageClass string) error {
	// Invalidate cache for all versions
	r.invalidateObjectCache(ctx, bucketName, objectKey, "") // For empty versionID
	// Note: In a real implementation, you might need to invalidate all versions

	q := query.Use(r.db).Object
	_, err := q.WithContext(ctx).Where(q.BucketName.Eq(bucketName), q.ObjectKey.Eq(objectKey)).Updates(map[string]interface{}{
		q.StorageClass.ColumnName().String(): storageClass,
		q.UpdatedAt.ColumnName().String():    time.Now(),
	})
	return repoerr.Wrap(err)
}

func (r *objectRepo) DeleteObject(ctx context.Context, bucketName, objectKey, versionID string) error {
	// Invalidate cache before delete
	r.invalidateObjectCache(ctx, bucketName, objectKey, versionID)

	q := query.Use(r.db)
	qs := q.Object.WithContext(ctx).Where(q.Object.BucketName.Eq(bucketName), q.Object.ObjectKey.Eq(objectKey))
	if versionID != "" {
		qs = qs.Where(q.Object.VersionID.Eq(versionID))
	} else {
		qs = qs.Where(q.Object.VersionID.Eq(""))
	}
	_, err := qs.Update(q.Object.Status, consts.ObjectStatusDeleted)
	return repoerr.Wrap(err)
}

func (r *objectRepo) GetByKeyWithTx(tx *gorm.DB, ctx context.Context, bucketName, objectKey, versionID string) (*do.ObjectDo, error) {
	q := query.Use(tx)
	qs := q.Object.WithContext(ctx).Where(q.Object.BucketName.Eq(bucketName), q.Object.ObjectKey.Eq(objectKey))
	if versionID != "" {
		qs = qs.Where(q.Object.VersionID.Eq(versionID))
	} else {
		qs = qs.Where(q.Object.VersionID.Eq(""))
	}

	modelObject, err := qs.First()
	if err != nil {
		return nil, repoerr.Wrap(err)
	}
	return r.toObjectDo(modelObject), nil
}

func (r *objectRepo) DeleteObjectWithTx(tx *gorm.DB, ctx context.Context, bucketName, objectKey, versionID string) error {
	q := query.Use(tx)
	qs := q.Object.WithContext(ctx).Where(q.Object.BucketName.Eq(bucketName), q.Object.ObjectKey.Eq(objectKey))
	if versionID != "" {
		qs = qs.Where(q.Object.VersionID.Eq(versionID))
	} else {
		qs = qs.Where(q.Object.VersionID.Eq(""))
	}
	_, err := qs.Update(q.Object.Status, consts.ObjectStatusDeleted)
	return repoerr.Wrap(err)
}
func (r *objectRepo) ListByBucketWithPrefix(ctx context.Context, list *do.ListObjectsByBucket) ([]*do.ObjectDo, error) {
	q := query.Use(r.db).Object

	qs := q.WithContext(ctx)

	if list.BucketID != 0 {
		qs = qs.Where(q.BucketID.Eq(list.BucketID))
	}

	if list.BucketName != "" {
		qs = qs.Where(q.BucketName.Eq(list.BucketName))
	}

	if list.Prefix != "" {
		qs = qs.Where(q.ObjectKey.Like(list.Prefix + "%"))
	}

	qs = qs.Order(q.ID.Asc()).
		Where(q.ID.Gt(list.Cursor)).
		Limit(list.Limit)

	modelObjects, err := qs.Find()
	if err != nil {
		return nil, repoerr.Wrap(err)
	}
	var objects []*do.ObjectDo
	for _, modelObject := range modelObjects {
		objects = append(objects, r.toObjectDo(modelObject))
	}
	return objects, nil
}
