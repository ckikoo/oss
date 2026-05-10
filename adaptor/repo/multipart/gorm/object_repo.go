package gorm

import (
	"context"
	"crypto/md5"
	"fmt"
	"time"

	"oss/adaptor/repo/model"
	"oss/adaptor/repo/multipart"
	"oss/adaptor/repo/query"
	"oss/service/do"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ObjectRepo struct {
	db *gorm.DB
}

var _ multipart.IMultipartRepo = (*ObjectRepo)(nil)

func NewObjectRepo(db *gorm.DB) *ObjectRepo {
	return &ObjectRepo{db: db}
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

func (r *ObjectRepo) GetMultipartUploadByID(ctx context.Context, userId int64, uploadID string) (*do.MultipartUploadDo, error) {
	q := query.Use(r.db).MultipartUpload

	expr := q.WithContext(ctx).Where(q.UploadID.Eq(uploadID))
	if userId != 0 {
		expr = expr.Where(q.UserID.Eq(userId))
	}

	modelUpload, err := expr.First()
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

func (r *ObjectRepo) UpdateMultipartUpload(ctx context.Context, userID int64, uploadID string, update *do.UpdateMultipartUpload) (*do.MultipartUploadDo, error) {
	db := r.db.WithContext(ctx)
	if err := r.updateMultipartUpload(ctx, db, userID, uploadID, update); err != nil {
		return nil, err
	}
	return r.GetMultipartUploadByID(ctx, userID, uploadID)
}

func (r *ObjectRepo) UpdateMultipartUploadWithTx(tx *gorm.DB, ctx context.Context, userID int64, uploadID string, update *do.UpdateMultipartUpload) error {
	return r.updateMultipartUpload(ctx, tx.WithContext(ctx), userID, uploadID, update)
}

func (r *ObjectRepo) updateMultipartUpload(ctx context.Context, db *gorm.DB, userID int64, uploadID string, update *do.UpdateMultipartUpload) error {
	qs := query.Use(db).MultipartUpload

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
		return gorm.ErrInvalidData
	}

	updates[qs.UpdatedAt.ColumnName().String()] = time.Now()

	if _, err := qs.WithContext(ctx).Where(qs.UploadID.Eq(uploadID), qs.UserID.Eq(userID)).Updates(updates); err != nil {
		return err
	}
	return nil
}

func (r *ObjectRepo) CreateOrUpdateMultipartPart(ctx context.Context, part *do.CreateMultipartPart) (bool, error) {
	modelPart := &model.MultipartPart{
		UploadID:    part.UploadID,
		PartNumber:  part.PartNumber,
		Size:        part.Size,
		Etag:        part.Etag,
		StoragePath: part.StoragePath,
		Status:      part.Status,
	}

	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "upload_id"}, {Name: "part_number"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"size":         part.Size,
			"etag":         part.Etag,
			"storage_path": part.StoragePath,
			"status":       part.Status,
		}),
	}).Create(modelPart)
	if result.Error != nil {
		return false, result.Error
	}

	// RowsAffected == 1 表示插入；2 表示更新。
	return result.RowsAffected == 1, nil
}

func (r *ObjectRepo) GetMultipartPart(ctx context.Context, userID int64, uploadID string, partNumber int32) (*do.MultipartPartDo, error) {
	// Verify ownership: check that the upload belongs to the user
	upload, err := r.GetMultipartUploadByID(ctx, userID, uploadID)
	if err != nil {
		return nil, err
	}
	if upload == nil {
		return nil, gorm.ErrRecordNotFound
	}

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

func (r *ObjectRepo) ListMultipartParts(ctx context.Context, userID int64, uploadID string) ([]*do.MultipartPartDo, error) {
	// Verify ownership: check that the upload belongs to the user
	upload, err := r.GetMultipartUploadByID(ctx, userID, uploadID)
	if err != nil {
		return nil, err
	}
	if upload == nil {
		return nil, gorm.ErrRecordNotFound
	}

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

func (r *ObjectRepo) DeleteMultipartParts(ctx context.Context, userID int64, uploadID string) error {
	// Verify ownership: check that the upload belongs to the user
	upload, err := r.GetMultipartUploadByID(ctx, userID, uploadID)
	if err != nil {
		return err
	}
	if upload == nil {
		return gorm.ErrRecordNotFound
	}

	modelPart := &model.MultipartPart{}
	return r.db.WithContext(ctx).Where("upload_id = ?", uploadID).Delete(modelPart).Error
}

func (r *ObjectRepo) DeleteMultipartPartsWithTx(tx *gorm.DB, ctx context.Context, userID int64, uploadID string) error {
	// Verify ownership: check that the upload belongs to the user
	upload, err := r.GetMultipartUploadByID(ctx, userID, uploadID)
	if err != nil {
		return err
	}
	if upload == nil {
		return gorm.ErrRecordNotFound
	}

	modelPart := &model.MultipartPart{}
	return tx.WithContext(ctx).Where("upload_id = ?", uploadID).Delete(modelPart).Error
}

// Helper function to generate object key hash
func GenerateObjectKeyHash(objectKey string) string {
	hash := md5.Sum([]byte(objectKey))
	return fmt.Sprintf("%x", hash)
}
