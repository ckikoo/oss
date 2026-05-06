package multipart

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"time"

	"oss/adaptor"
	"oss/adaptor/repo/model"
	"oss/adaptor/repo/query"
	"oss/service/do"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type ObjectRepo struct {
	db *gorm.DB
}

var _ IMultipartRepo = (*ObjectRepo)(nil)

func NewObjectRepo(adaptor adaptor.IAdaptor) *ObjectRepo {
	sqlDB := adaptor.GetDB()
	ormDB, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		panic(err)
	}

	return &ObjectRepo{db: ormDB}
}

func (r *ObjectRepo) CreateMultipartUpload(ctx context.Context, upload *do.CreateMultipartUpload) (int64, error) {
	qs := query.Use(r.db).MultipartUpload
	modelUpload := &model.MultipartUpload{
		UploadID:      upload.UploadID,
		BucketID:      upload.BucketID,
		BucketName:    upload.BucketName,
		ObjectKey:     upload.ObjectKey,
		ObjectKeyHash: upload.ObjectKeyHash,
		UserID:        upload.UserID,
		TotalChunk:    upload.TotalChunk,
		Status:        upload.Status,
		StorageClass:  upload.StorageClass,
		ContentType:   upload.ContentType,
		Metadata:      upload.Metadata,
		ExpiresAt:     upload.ExpiresAt,
		LastActiveAt:  upload.LastActiveAt,
	}
	if err := qs.WithContext(ctx).Create(modelUpload); err != nil {
		return 0, err
	}
	return modelUpload.ID, nil
}

func (r *ObjectRepo) GetMultipartUploadByID(ctx context.Context, uploadID string) (*do.MultipartUploadDo, error) {
	q := query.Use(r.db)
	modelUpload, err := q.MultipartUpload.WithContext(ctx).Where(q.MultipartUpload.UploadID.Eq(uploadID)).First()
	if err != nil {
		return nil, err
	}
	return &do.MultipartUploadDo{
		ID:            modelUpload.ID,
		UploadID:      modelUpload.UploadID,
		BucketID:      modelUpload.BucketID,
		BucketName:    modelUpload.BucketName,
		ObjectKey:     modelUpload.ObjectKey,
		ObjectKeyHash: modelUpload.ObjectKeyHash,
		UserID:        modelUpload.UserID,
		TotalChunk:    modelUpload.TotalChunk,
		Status:        modelUpload.Status,
		StorageClass:  modelUpload.StorageClass,
		ContentType:   modelUpload.ContentType,
		Metadata:      modelUpload.Metadata,
		ExpiresAt:     modelUpload.ExpiresAt,
		LastActiveAt:  modelUpload.LastActiveAt,
		CreatedAt:     modelUpload.CreatedAt,
		UpdatedAt:     modelUpload.UpdatedAt,
	}, nil
}

func (r *ObjectRepo) UpdateMultipartUpload(ctx context.Context, uploadID string, update *do.UpdateMultipartUpload) (*do.MultipartUploadDo, error) {
	qs := query.Use(r.db).MultipartUpload

	updates := map[string]interface{}{}
	if update.TotalChunk != nil {
		updates[qs.TotalChunk.ColumnName().String()] = *update.TotalChunk
	}

	if update.Status != nil {
		updates[qs.Status.ColumnName().String()] = *update.Status
	}
	if update.StorageClass != nil {
		updates[qs.StorageClass.ColumnName().String()] = *update.StorageClass
	}
	if update.ContentType != nil {
		updates[qs.ContentType.ColumnName().String()] = *update.ContentType
	}
	if update.Metadata != nil {
		updates[qs.Metadata.ColumnName().String()] = *update.Metadata
	}
	if update.ExpiresAt != nil {
		updates[qs.ExpiresAt.ColumnName().String()] = *update.ExpiresAt
	}
	if update.LastActiveAt != nil {
		updates[qs.LastActiveAt.ColumnName().String()] = *update.LastActiveAt
	}

	if len(updates) == 0 {
		return nil, gorm.ErrInvalidData
	}

	updates[qs.UpdatedAt.ColumnName().String()] = time.Now()

	if _, err := qs.WithContext(ctx).Where(qs.UploadID.Eq(uploadID)).Updates(updates); err != nil {
		return nil, err
	}
	return r.GetMultipartUploadByID(ctx, uploadID)
}

func (r *ObjectRepo) CreateOrUpdateMultipartPart(ctx context.Context, part *do.CreateMultipartPart) (bool, error) {
	q := query.Use(r.db).MultipartPart
	modelPart, err := q.WithContext(ctx).Where(q.UploadID.Eq(part.UploadID), q.PartNumber.Eq(part.PartNumber)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			modelPart = &model.MultipartPart{
				UploadID:    part.UploadID,
				PartNumber:  part.PartNumber,
				Size:        part.Size,
				Etag:        part.Etag,
				StoragePath: part.StoragePath,
				Status:      part.Status,
			}

			if err := r.db.WithContext(ctx).Create(modelPart).Error; err != nil {
				return false, err
			}
			return true, nil
		}
		return false, err
	}

	updates := map[string]interface{}{
		q.Size.ColumnName().String():        part.Size,
		q.Etag.ColumnName().String():        part.Etag,
		q.StoragePath.ColumnName().String(): part.StoragePath,
		q.Status.ColumnName().String():      part.Status,
	}
	if err := r.db.WithContext(ctx).Model(&model.MultipartPart{}).
		Where(q.UploadID.Eq(part.UploadID), q.PartNumber.Eq(part.PartNumber)).
		Updates(updates).Error; err != nil {
		return false, err
	}
	return false, nil
}

func (r *ObjectRepo) GetMultipartPart(ctx context.Context, uploadID string, partNumber int32) (*do.MultipartPartDo, error) {
	q := query.Use(r.db)
	modelPart, err := q.MultipartPart.WithContext(ctx).Where(q.MultipartPart.UploadID.Eq(uploadID), q.MultipartPart.PartNumber.Eq(partNumber)).First()
	if err != nil {
		return nil, err
	}
	return &do.MultipartPartDo{
		ID:          modelPart.ID,
		UploadID:    modelPart.UploadID,
		PartNumber:  modelPart.PartNumber,
		Size:        modelPart.Size,
		Etag:        modelPart.Etag,
		StoragePath: modelPart.StoragePath,
		Status:      modelPart.Status,
		CreatedAt:   modelPart.CreatedAt,
	}, nil
}

func (r *ObjectRepo) ListMultipartParts(ctx context.Context, uploadID string) ([]*do.MultipartPartDo, error) {
	q := query.Use(r.db)
	modelParts, err := q.MultipartPart.WithContext(ctx).Where(q.MultipartPart.UploadID.Eq(uploadID)).Order(q.MultipartPart.PartNumber).Find()
	if err != nil {
		return nil, err
	}
	parts := make([]*do.MultipartPartDo, len(modelParts))
	for i, modelPart := range modelParts {
		parts[i] = &do.MultipartPartDo{
			ID:          modelPart.ID,
			UploadID:    modelPart.UploadID,
			PartNumber:  modelPart.PartNumber,
			Size:        modelPart.Size,
			Etag:        modelPart.Etag,
			StoragePath: modelPart.StoragePath,
			Status:      modelPart.Status,
			CreatedAt:   modelPart.CreatedAt,
		}
	}
	return parts, nil
}

func (r *ObjectRepo) DeleteMultipartParts(ctx context.Context, uploadID string) error {
	modelPart := &model.MultipartPart{}
	return r.db.WithContext(ctx).Where("upload_id = ?", uploadID).Delete(modelPart).Error
}

func (r *ObjectRepo) DeleteMultipartPartsWithTx(tx *gorm.DB, ctx context.Context, uploadID string) error {
	modelPart := &model.MultipartPart{}
	return tx.WithContext(ctx).Where("upload_id = ?", uploadID).Delete(modelPart).Error
}

// Helper function to generate object key hash
func GenerateObjectKeyHash(objectKey string) string {
	hash := md5.Sum([]byte(objectKey))
	return fmt.Sprintf("%x", hash)
}
