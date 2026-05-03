package object

import (
	"context"
	"time"

	"oss/adaptor"
	"oss/adaptor/repo/model"
	"oss/adaptor/repo/query"
	"oss/consts"
	"oss/service/do"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type ObjectRepo struct {
	db *gorm.DB
}

var _ IObjectRepo = (*ObjectRepo)(nil)

func NewObjectRepo(adaptor adaptor.IAdaptor) *ObjectRepo {
	sqlDB := adaptor.GetDB()
	ormDB, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		panic(err)
	}

	return &ObjectRepo{db: ormDB}
}

func (r *ObjectRepo) CreateObject(ctx context.Context, object *do.CreateObject) (int64, error) {

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
			return err
		}

		objectID = modelObject.ID

		result := tx.Model(&model.Bucket{}).Where(qsBucket.ID.Eq(object.BucketID)).Updates(map[string]interface{}{
			qsBucket.ObjectCount.ColumnName().String(): qsBucket.ObjectCount.Add(1),
			qsBucket.StorageSize.ColumnName().String(): qsBucket.StorageSize.Add(object.Size),
		})

		return result.Error
	})

	if err != nil {
		return 0, err
	}

	return objectID, nil
}

func (r *ObjectRepo) GetByKey(ctx context.Context, bucketName, objectKey, versionID string) (*do.ObjectDo, error) {
	q := query.Use(r.db)
	qs := q.Object.WithContext(ctx).Where(q.Object.BucketName.Eq(bucketName), q.Object.ObjectKey.Eq(objectKey))
	if versionID != "" {
		qs = qs.Where(q.Object.VersionID.Eq(versionID))
	} else {
		qs = qs.Where(q.Object.VersionID.Eq(""))
	}

	modelObject, err := qs.First()
	if err != nil {
		return nil, err
	}
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
	}, nil
}

func (r *ObjectRepo) ListByFilter(ctx context.Context, bucketName, prefix, delimiter, marker string, maxKeys int, versionID string) ([]*do.ObjectDo, error) {
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
		return nil, err
	}

	objects := make([]*do.ObjectDo, len(modelObjects))
	for i, modelObject := range modelObjects {
		objects[i] = &do.ObjectDo{
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
	return objects, nil
}

func (r *ObjectRepo) UpdateObject(ctx context.Context, bucketName, objectKey, versionID string, update *do.UpdateObject) (*do.ObjectDo, error) {
	updates := map[string]interface{}{}
	if update.Size != nil {
		updates["size"] = *update.Size
	}
	if update.Etag != nil {
		updates["etag"] = *update.Etag
	}
	if update.ContentType != nil {
		updates["content_type"] = *update.ContentType
	}
	if update.StorageClass != nil {
		updates["storage_class"] = *update.StorageClass
	}
	if update.StoragePath != nil {
		updates["storage_path"] = *update.StoragePath
	}
	if update.Acl != nil {
		updates["acl"] = *update.Acl
	}
	if update.Metadata != nil {
		updates["metadata"] = *update.Metadata
	}
	if update.Status != nil {
		updates["status"] = *update.Status
	}
	if len(updates) == 0 {
		return nil, gorm.ErrInvalidData
	}
	updates["updated_at"] = time.Now()

	q := query.Use(r.db)
	qs := q.Object.WithContext(ctx).Where(q.Object.BucketName.Eq(bucketName), q.Object.ObjectKey.Eq(objectKey))
	if versionID != "" {
		qs = qs.Where(q.Object.VersionID.Eq(versionID))
	} else {
		qs = qs.Where(q.Object.VersionID.Eq(""))
	}

	_, err := qs.Updates(updates)
	if err != nil {
		return nil, err
	}
	return r.GetByKey(ctx, bucketName, objectKey, versionID)
}

func (r *ObjectRepo) DeleteObject(ctx context.Context, bucketName, objectKey, versionID string) error {
	q := query.Use(r.db)
	qs := q.Object.WithContext(ctx).Where(q.Object.BucketName.Eq(bucketName), q.Object.ObjectKey.Eq(objectKey))
	if versionID != "" {
		qs = qs.Where(q.Object.VersionID.Eq(versionID))
	} else {
		qs = qs.Where(q.Object.VersionID.Eq(""))
	}
	_, err := qs.Update(q.Object.Status, consts.ObjectStatusDeleted)
	return err
}
