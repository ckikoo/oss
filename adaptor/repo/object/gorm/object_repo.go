package gorm

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"

	"oss/adaptor"
	"oss/adaptor/repo/model"
	"oss/adaptor/repo/object"
	"oss/adaptor/repo/query"
	"oss/adaptor/repo/repocache"
	"oss/adaptor/repo/repoerr"
	"oss/adaptor/tx"
	"oss/consts"
	"oss/service/do"
	"oss/utils/cache"
	"oss/utils/logger"
	"oss/utils/tools"

	"github.com/go-redis/redis/v8"
	"github.com/gogf/gf/util/gconv"
	"go.uber.org/zap"
	"gorm.io/gen/field"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type objectRepo struct {
	db           *gorm.DB
	q            *query.Query
	rds          *redis.Client
	cacheManager cache.IManager
	g            *singleflight.Group
	cacheEnabled bool
}

var _ object.IObjectRepo = (*objectRepo)(nil)

func NewObjectRepo(a adaptor.IAdaptor) object.IObjectRepo {
	return &objectRepo{
		q:            query.Use(a.GetGORM()),
		db:           a.GetGORM(),
		rds:          a.GetRedis(),
		cacheManager: a.GetCache(),
		g:            &singleflight.Group{},
		cacheEnabled: true,
	}
}

func (r *objectRepo) WithTx(tx tx.Tx) object.IObjectRepo {
	db := tx.(*gorm.DB)
	return &objectRepo{
		q:            query.Use(db),
		db:           db,
		rds:          r.rds,
		cacheManager: r.cacheManager,
		g:            r.g,
		cacheEnabled: false,
	}
}
func (r *objectRepo) toObjectDo(modelObject *model.Object) *do.ObjectDo {
	return &do.ObjectDo{
		ID:            modelObject.ID,
		BucketID:      modelObject.BucketID,
		BucketName:    modelObject.BucketName,
		ParentID:      modelObject.ParentID,
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
		IsDir:         modelObject.IsDir,
		IsLatest:      modelObject.IsLatest,
		Status:        modelObject.Status,
		AccessCount:   modelObject.AccessCount,
		CreatedAt:     modelObject.CreatedAt,
		UpdatedAt:     modelObject.UpdatedAt,
		DeletedAt:     modelObject.DeletedAt,
	}
}

// ─── Cache Helpers ────────────────────────────────────────────────────

// getCachedRedis retrieves object from Redis cache, returns nil if not found
func (r *objectRepo) getCachedRedis(ctx context.Context, key string) *do.ObjectDo {
	if r.rds == nil {
		return nil
	}
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

// setCachedRedis stores object in cache with TTL
func (r *objectRepo) setCachedRedis(ctx context.Context, key string, object *do.ObjectDo) error {
	data, err := json.Marshal(object)
	if err != nil {
		return repoerr.Wrap(err)
	}
	return repoerr.Wrap(r.rds.Set(ctx, key, data, time.Duration(consts.CacheTTLObject)*time.Second).Err())
}

// setAllCaches 本地 + Redis 同时写入
func (r *objectRepo) setAllCaches(ctx context.Context, keys []string, obj *do.ObjectDo) {
	if r.cacheManager == nil {
		return
	}
	for _, key := range keys {
		r.cacheManager.Set(key, obj, 0) // TTL=0 使用 manager 默认值

		if err := r.setCachedRedis(ctx, key, obj); err != nil {
			logger.Warn("failed to set object redis cache",
				zap.Error(err),
				zap.String("key", key),
				zap.String("object", gconv.String(obj)),
			)
		}
	}
}

func (r *objectRepo) getLatestVersion(ctx context.Context, bucketName, objectKey string) (string, error) {
	if r.rds == nil {
		return "", redis.Nil
	}
	return r.rds.Get(ctx, consts.ObjectLatestVersionCacheKey(bucketName, objectKey)).Result()
}
func (r *objectRepo) setLatestVersion(ctx context.Context, bucketName, objectKey string, version string) error {
	if r.rds == nil {
		return nil
	}
	return r.rds.Set(ctx, consts.ObjectLatestVersionCacheKey(bucketName, objectKey), version, time.Duration(consts.CacheTTLObject)*time.Second).Err()
}

// invalidateObjectCache 删本地 + 删 Redis + 广播其他实例
func (r *objectRepo) invalidateObjectCache(ctx context.Context, bucketName, objectKey string, versionIDs ...string) {
	keySet := map[string]struct{}{
		consts.ObjectCacheKey(bucketName, objectKey, ""):          {},
		consts.ObjectLatestVersionCacheKey(bucketName, objectKey): {},
	}
	for _, versionID := range versionIDs {
		keySet[consts.ObjectCacheKey(bucketName, objectKey, versionID)] = struct{}{}
	}
	keys := make([]string, 0, len(keySet))
	for key := range keySet {
		keys = append(keys, key)
	}
	repocache.Invalidator{
		RDS:          r.rds,
		Local:        r.cacheManager,
		DoubleDelete: true,
		LogName:      "object",
	}.AfterCommit(ctx, keys...)
}

// ─── 三层查询核心 ────────────────────────────────────────────────────────────

// getByKey 统一的三层缓存查询：本地 → Redis → singleflight → DB
func (r *objectRepo) getByKey(
	ctx context.Context,
	cacheKey string,
	queryFn func() (*do.ObjectDo, error),
) (*do.ObjectDo, error) {
	return repocache.Accessor[*do.ObjectDo]{
		RDS:     r.rds,
		Local:   r.cacheManager,
		Group:   r.g,
		TTL:     time.Duration(consts.CacheTTLObject) * time.Second,
		Enabled: r.cacheEnabled,
		LogName: "object",
	}.Get(ctx, cacheKey, func(context.Context) (*do.ObjectDo, error) {
		return queryFn()
	})
}

// repoerr.Wrap maps GORM errors to repoerr sentinels

func (r *objectRepo) CreateObject(ctx context.Context, object *do.CreateObject) (int64, error) {

	var (
		objectID int64 = 0
		err      error
	)
	now := time.Now()
	parentID, err := r.resolveParentID(ctx, object.BucketID, object.BucketName, object.ObjectKey, object.ParentID, now)
	if err != nil {
		return 0, err
	}
	modelObject := &model.Object{
		BucketID:      object.BucketID,
		BucketName:    object.BucketName,
		ParentID:      parentID,
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
		IsDir:         object.IsDir,
		Status:        consts.ObjectStatusNormal,
		IsLatest:      1,
		AccessCount:   0,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	q := query.Use(r.db).Object
	if err = q.WithContext(ctx).Omit(q.LatestGuard).Create(modelObject); err != nil {
		return 0, repoerr.Wrap(err)
	}

	objectID = modelObject.ID

	return objectID, nil
}

func (r *objectRepo) CreateDeleteMarker(ctx context.Context, marker *do.CreateDeleteMarker) (int64, error) {
	now := time.Now()
	parentID, err := r.resolveParentID(ctx, marker.BucketID, marker.BucketName, marker.ObjectKey, marker.ParentID, now)
	if err != nil {
		return 0, err
	}
	modelObject := &model.Object{
		BucketID:      marker.BucketID,
		BucketName:    marker.BucketName,
		ParentID:      parentID,
		ObjectKey:     marker.ObjectKey,
		ObjectKeyHash: marker.ObjectKeyHash,
		VersionID:     marker.VersionID,
		Size:          0,
		Etag:          "",
		ContentType:   nil,
		StorageClass:  marker.StorageClass,
		IsMultipart:   consts.ObjectIsMultipartNormal,
		UploadID:      nil,
		StoragePath:   nil,
		Acl:           marker.Acl,
		Metadata:      marker.Metadata,
		IsDir:         consts.ObjectIsDirNo,
		IsLatest:      1,
		Status:        consts.ObjectStatusDeleteMark,
		AccessCount:   0,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	q := query.Use(r.db).Object
	if err := q.WithContext(ctx).Omit(q.LatestGuard).Create(modelObject); err != nil {
		return 0, repoerr.Wrap(err)
	}

	r.invalidateObjectCache(ctx, marker.BucketName, marker.ObjectKey, marker.VersionID)
	return modelObject.ID, nil
}

func (r *objectRepo) resolveParentID(ctx context.Context, bucketID int64, bucketName, objectKey string, explicitParentID *int64, now time.Time) (*int64, error) {
	if explicitParentID != nil {
		return explicitParentID, nil
	}
	return r.ensureDirectoryPath(ctx, bucketID, bucketName, objectKey, now)
}

func (r *objectRepo) ensureDirectoryPath(ctx context.Context, bucketID int64, bucketName, objectKey string, now time.Time) (*int64, error) {
	var parentID *int64
	for _, dirKey := range parentDirectoryKeys(objectKey) {
		dirID, err := r.ensureDirectory(ctx, bucketID, bucketName, dirKey, parentID, now)
		if err != nil {
			return nil, err
		}
		parentID = &dirID
	}
	return parentID, nil
}

func (r *objectRepo) ensureDirectory(ctx context.Context, bucketID int64, bucketName, dirKey string, parentID *int64, now time.Time) (int64, error) {
	objectKeyHash := tools.Md5Hash(dirKey)
	modelObject := &model.Object{
		BucketID:      bucketID,
		BucketName:    bucketName,
		ParentID:      parentID,
		ObjectKey:     dirKey,
		ObjectKeyHash: objectKeyHash,
		VersionID:     directoryVersionID(objectKeyHash),
		Size:          0,
		Etag:          "",
		ContentType:   nil,
		StorageClass:  consts.StorageClassStandard,
		IsMultipart:   consts.ObjectIsMultipartNormal,
		UploadID:      nil,
		StoragePath:   nil,
		Acl:           consts.ObjectAclInheritBucket,
		Metadata:      nil,
		IsDir:         consts.ObjectIsDirYes,
		Status:        consts.ObjectStatusNormal,
		IsLatest:      1,
		AccessCount:   0,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Omit("latest_guard").
		Create(modelObject).Error; err != nil {
		return 0, repoerr.Wrap(err)
	}
	if modelObject.ID > 0 {
		return modelObject.ID, nil
	}
	return r.getDirectoryID(ctx, bucketName, dirKey)
}

func (r *objectRepo) getDirectoryID(ctx context.Context, bucketName, dirKey string) (int64, error) {
	q := query.Use(r.db).Object
	var dirID int64
	err := q.WithContext(ctx).
		Select(q.ID).
		Where(
			q.BucketName.Eq(bucketName),
			q.ObjectKey.Eq(dirKey),
			q.IsDir.Eq(consts.ObjectIsDirYes),
			q.Status.Eq(consts.ObjectStatusNormal),
			q.IsLatest.Eq(1),
		).
		Scan(&dirID)
	if err != nil {
		return 0, repoerr.Wrap(err)
	}
	if dirID == 0 {
		return 0, repoerr.ErrNotFound
	}
	return dirID, nil
}

func parentDirectoryKeys(objectKey string) []string {
	key := strings.TrimLeft(objectKey, "/")
	key = strings.TrimRight(key, "/")
	if key == "" {
		return nil
	}

	parts := strings.Split(key, "/")
	if len(parts) <= 1 {
		return nil
	}

	dirs := make([]string, 0, len(parts)-1)
	var b strings.Builder
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "" {
			continue
		}
		b.WriteString(parts[i])
		b.WriteByte('/')
		dirs = append(dirs, b.String())
	}
	return dirs
}

func directoryVersionID(objectKeyHash string) string {
	if len(objectKeyHash) <= 29 {
		return "dir" + objectKeyHash
	}
	return "dir" + objectKeyHash[:29]
}

// version 为空，默认返回最新的
func (r *objectRepo) GetByKey(ctx context.Context, bucketName, objectKey, versionID string) (*do.ObjectDo, error) {
	cacheKey := consts.ObjectCacheKey(bucketName, objectKey, versionID)

	return r.getByKey(ctx, cacheKey, func() (*do.ObjectDo, error) {
		// Cache miss, query database
		q := query.Use(r.db)
		qs := q.Object.WithContext(ctx).Where(
			q.Object.BucketName.Eq(bucketName),
			q.Object.ObjectKey.Eq(objectKey),
			q.Object.IsDir.Eq(consts.ObjectIsDirNo),
			q.Object.Status.Neq(consts.ObjectStatusDeleted),
		)
		if versionID != "" {
			qs = qs.Where(q.Object.VersionID.Eq(versionID))
		} else {
			qs = qs.Where(q.Object.IsLatest.Eq(1))
		}

		modelObject, err := qs.First()
		if err != nil {
			return nil, repoerr.Wrap(err)
		}

		return r.toObjectDo(modelObject), nil
	})
}

func (r *objectRepo) GetByIDAndVersion(ctx context.Context, objectID int64, versionID string) (*do.ObjectDo, error) {
	if objectID <= 0 || versionID == "" {
		return nil, repoerr.ErrInvalidData
	}

	q := query.Use(r.db).Object
	modelObject, err := q.WithContext(ctx).
		Where(q.ID.Eq(objectID), q.VersionID.Eq(versionID)).
		First()
	if err != nil {
		return nil, repoerr.Wrap(err)
	}
	return r.toObjectDo(modelObject), nil
}

func (r *objectRepo) ListByFilter(ctx context.Context, filter *do.ListObjectsFilter) ([]*do.ObjectDo, error) {
	q := query.Use(r.db)
	qs := q.Object.WithContext(ctx).Where(q.Object.BucketName.Eq(filter.BucketName), q.Object.Status.Eq(consts.ObjectStatusNormal))
	if filter.BucketID != 0 {
		qs = qs.Where(q.Object.BucketID.Eq(filter.BucketID))
	}

	if filter.UseParentID {
		if filter.Prefix != "" {
			dirID, err := r.getDirectoryID(ctx, filter.BucketName, filter.Prefix)
			if err != nil {
				if err == repoerr.ErrNotFound {
					return []*do.ObjectDo{}, nil
				}
				return nil, err
			}
			filter.ParentID = &dirID
		}
		if filter.ParentID != nil {
			qs = qs.Where(q.Object.ParentID.Eq(*filter.ParentID))
		} else {
			qs = qs.Where(q.Object.ParentID.IsNull())
		}
	} else {
		qs = qs.Where(q.Object.IsDir.Eq(consts.ObjectIsDirNo))
	}

	if filter.Prefix != "" && !filter.UseParentID {
		qs = qs.Where(q.Object.ObjectKey.Like(filter.Prefix + "%"))
	}
	if filter.DirectoryOrder && filter.CursorKey != "" && filter.CursorID > 0 {
		qs = qs.Where(field.Or(
			q.Object.IsDir.Lt(filter.CursorIsDir),
			field.And(q.Object.IsDir.Eq(filter.CursorIsDir), q.Object.ObjectKey.Gt(filter.CursorKey)),
			field.And(q.Object.IsDir.Eq(filter.CursorIsDir), q.Object.ObjectKey.Eq(filter.CursorKey), q.Object.ID.Gt(filter.CursorID)),
		))
	} else if filter.Marker != "" {
		qs = qs.Where(q.Object.ObjectKey.Gt(filter.Marker))
	}
	if filter.VersionID != "" {
		qs = qs.Where(q.Object.VersionID.Eq(filter.VersionID))
	} else {
		qs = qs.Where(q.Object.IsLatest.Eq(1))
	}
	if filter.StorageClass != "" {
		qs = qs.Where(q.Object.StorageClass.Eq(filter.StorageClass))
	}
	if filter.ContentType != "" {
		qs = qs.Where(q.Object.ContentType.Eq(filter.ContentType))
	}
	if !filter.CreatedAtStart.IsZero() {
		qs = qs.Where(q.Object.CreatedAt.Gte(filter.CreatedAtStart))
	}
	if !filter.CreatedAtEnd.IsZero() {
		qs = qs.Where(q.Object.CreatedAt.Lte(filter.CreatedAtEnd))
	}

	if filter.Limit > 0 {
		qs = qs.Limit(filter.Limit)
	} else {
		qs = qs.Limit(consts.DefaultMaxKeys)
	}

	if filter.DirectoryOrder {
		qs = qs.Order(q.Object.IsDir.Desc(), q.Object.ObjectKey, q.Object.ID)
	} else {
		qs = qs.Order(q.Object.ObjectKey)
	}

	modelObjects, err := qs.Find()
	if err != nil {
		return nil, repoerr.Wrap(err)
	}

	objects := make([]*do.ObjectDo, len(modelObjects))
	for i, modelObject := range modelObjects {
		objects[i] = r.toObjectDo(modelObject)
	}
	return objects, nil
}

func (r *objectRepo) ListVersionsByFilter(ctx context.Context, bucketName, objectKey string) ([]*do.ObjectDo, error) {
	q := query.Use(r.db).Object
	qs := q.WithContext(ctx).Where(q.BucketName.Eq(bucketName), q.ObjectKey.Eq(objectKey))
	qs = qs.Where(q.Status.Neq(consts.ObjectStatusDeleted))
	modelObjects, err := qs.Order(q.ID.Desc()).Find()
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

	if bucketName == "" || objectKey == "" || versionID == "" {
		return nil, repoerr.ErrInvalidData
	}

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

	if update.IsLatest != nil {
		updates[q.IsLatest.ColumnName().String()] = *update.IsLatest
	}

	if len(updates) == 0 {
		return nil, gorm.ErrInvalidData
	}

	updates[q.UpdatedAt.ColumnName().String()] = time.Now()

	qs := q.WithContext(ctx).Where(q.BucketName.Eq(bucketName), q.ObjectKey.Eq(objectKey), q.VersionID.Eq(versionID))

	_, err := qs.Updates(updates)
	if err != nil {
		return nil, repoerr.Wrap(err)
	}

	r.invalidateObjectCache(ctx, bucketName, objectKey, versionID)

	return r.GetByKey(ctx, bucketName, objectKey, versionID)
}

func (r *objectRepo) UpdateObjectStorageClass(ctx context.Context, bucketName, objectKey, storageClass string) error {
	q := query.Use(r.db).Object
	_, err := q.WithContext(ctx).Where(q.BucketName.Eq(bucketName), q.ObjectKey.Eq(objectKey)).Updates(map[string]interface{}{
		q.StorageClass.ColumnName().String(): storageClass,
		q.UpdatedAt.ColumnName().String():    time.Now(),
	})
	if err != nil {
		return repoerr.Wrap(err)
	}
	r.invalidateObjectCache(ctx, bucketName, objectKey, "")
	return nil
}

func (r *objectRepo) DeleteObject(ctx context.Context, bucketName, objectKey string, versionID ...string) error {

	q := query.Use(r.db).Object
	qs := q.WithContext(ctx).Where(q.BucketName.Eq(bucketName), q.ObjectKey.Eq(objectKey))
	if len(versionID) > 0 {
		qs = qs.Where(q.VersionID.In(versionID...))
	}

	updates := map[string]interface{}{
		q.Status.ColumnName().String():    consts.ObjectStatusDeleted,
		q.IsLatest.ColumnName().String():  0,
		"deleted_at":                      time.Now(),
		q.UpdatedAt.ColumnName().String(): time.Now(),
	}

	_, err := qs.Updates(updates)
	if err != nil {
		return repoerr.Wrap(err)
	}

	r.invalidateObjectCache(ctx, bucketName, objectKey, versionID...)

	return nil
}

func (r *objectRepo) MarkAllNotLatest(ctx context.Context, bucketName, objectKey string) error {
	q := query.Use(r.db).Object
	_, err := q.WithContext(ctx).
		Where(q.BucketName.Eq(bucketName), q.ObjectKey.Eq(objectKey), q.Status.Neq(consts.ObjectStatusDeleted)).
		Update(q.IsLatest, 0)
	if err != nil {
		return repoerr.Wrap(err)
	}

	r.invalidateObjectCache(ctx, bucketName, objectKey)
	return nil
}

func (r *objectRepo) MarkVersionPurged(ctx context.Context, bucketName, objectKey, versionID string) (*do.ObjectDo, error) {
	if bucketName == "" || objectKey == "" || versionID == "" {
		return nil, repoerr.ErrInvalidData
	}

	q := query.Use(r.db).Object
	modelObject, err := q.WithContext(ctx).
		Where(q.BucketName.Eq(bucketName), q.ObjectKey.Eq(objectKey), q.VersionID.Eq(versionID), q.Status.Neq(consts.ObjectStatusDeleted)).
		First()
	if err != nil {
		return nil, repoerr.Wrap(err)
	}

	now := time.Now()
	updates := map[string]interface{}{
		q.Status.ColumnName().String():    consts.ObjectStatusDeleted,
		q.IsLatest.ColumnName().String():  0,
		"deleted_at":                      now,
		q.UpdatedAt.ColumnName().String(): now,
	}
	if _, err := q.WithContext(ctx).
		Where(q.ID.Eq(modelObject.ID)).
		Updates(updates); err != nil {
		return nil, repoerr.Wrap(err)
	}

	r.invalidateObjectCache(ctx, bucketName, objectKey, versionID)
	return r.toObjectDo(modelObject), nil
}

func (r *objectRepo) PromotePreviousVersion(ctx context.Context, bucketName, objectKey string) (*do.ObjectDo, error) {
	q := query.Use(r.db).Object
	modelObjects, err := q.WithContext(ctx).
		Where(q.BucketName.Eq(bucketName), q.ObjectKey.Eq(objectKey), q.Status.Neq(consts.ObjectStatusDeleted)).
		Order(q.ID.Desc()).
		Limit(1).
		Find()
	if err != nil {
		return nil, repoerr.Wrap(err)
	}
	if len(modelObjects) == 0 {
		r.invalidateObjectCache(ctx, bucketName, objectKey)
		return nil, nil
	}

	modelObject := modelObjects[0]
	if _, err := q.WithContext(ctx).
		Where(q.ID.Eq(modelObject.ID)).
		Update(q.IsLatest, 1); err != nil {
		return nil, repoerr.Wrap(err)
	}

	r.invalidateObjectCache(ctx, bucketName, objectKey, modelObject.VersionID)
	modelObject.IsLatest = 1
	return r.toObjectDo(modelObject), nil
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
		Where(q.Status.Eq(consts.ObjectStatusNormal), q.IsLatest.Eq(1), q.IsDir.Eq(consts.ObjectIsDirNo)).
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

// 效率有点地下降
func (r *objectRepo) UpdateObjectNotLatest(ctx context.Context, bucketName, objectKey string, version string) error {
	return r.MarkAllNotLatest(ctx, bucketName, objectKey)
}

// 只要versionID，不需要其他字段
// GetLastVersion 获取对象最新的 VersionID
func (r *objectRepo) GetLastVersion(ctx context.Context, bucketName, objectKey string) (string, error) {
	cacheKey := consts.ObjectLatestVersionCacheKey(bucketName, objectKey)
	if !r.cacheEnabled || r.rds == nil || r.cacheManager == nil || r.g == nil {
		return r.getLastVersionDB(ctx, bucketName, objectKey)
	}

	// ① 本地缓存
	if entry, ok := r.cacheManager.Get(cacheKey); ok {
		if version, ok := entry.Data.(string); ok {
			return version, nil
		}
		// 类型不对，清理脏数据
		r.cacheManager.Remove(cacheKey)
	}

	// ② 使用 singleflight 防止缓存击穿
	result, err, _ := r.g.Do(cacheKey, func() (interface{}, error) {
		// ②.1 再查一次 Redis（可能其他 goroutine 已经回填）
		if version, err := r.getLatestVersion(ctx, bucketName, objectKey); err == nil && version != "" {
			r.cacheManager.Set(cacheKey, version, consts.CacheTTLObject)
			return version, nil
		}

		versionID, err := r.getLastVersionDB(ctx, bucketName, objectKey)
		if err != nil {
			return "", err
		}

		r.cacheManager.Set(cacheKey, versionID, consts.CacheTTLObject)

		return versionID, nil
	})

	if err != nil {
		return "", repoerr.Wrap(err)
	}

	return result.(string), nil
}

func (r *objectRepo) getLastVersionDB(ctx context.Context, bucketName, objectKey string) (string, error) {
	q := query.Use(r.db).Object
	var versionID string

	err := q.WithContext(ctx).
		Select(q.VersionID).
		Where(q.BucketName.Eq(bucketName), q.ObjectKey.Eq(objectKey)).
		Where(q.Status.Neq(consts.ObjectStatusDeleted)).
		Where(q.IsLatest.Eq(1)).
		Scan(&versionID)
	if err != nil {
		return "", err
	}
	if versionID == "" {
		return "", repoerr.ErrNotFound
	}
	return versionID, nil
}
