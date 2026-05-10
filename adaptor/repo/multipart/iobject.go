package multipart

import (
	"context"
	"oss/service/do"

	"gorm.io/gorm"
)

type IMultipartRepo interface {
	CreateMultipartUpload(ctx context.Context, upload *do.CreateMultipartUpload) (int64, error)
	GetMultipartUploadByID(ctx context.Context, userId int64, uploadID string) (*do.MultipartUploadDo, error)
	UpdateMultipartUpload(ctx context.Context, userID int64, uploadID string, update *do.UpdateMultipartUpload) (*do.MultipartUploadDo, error)
	UpdateMultipartUploadWithTx(tx *gorm.DB, ctx context.Context, userID int64, uploadID string, update *do.UpdateMultipartUpload) error
	CreateOrUpdateMultipartPart(ctx context.Context, part *do.CreateMultipartPart) (bool, error)
	GetMultipartPart(ctx context.Context, userID int64, uploadID string, partNumber int32) (*do.MultipartPartDo, error)
	ListMultipartParts(ctx context.Context, userID int64, uploadID string) ([]*do.MultipartPartDo, error)
	DeleteMultipartParts(ctx context.Context, userID int64, uploadID string) error
	DeleteMultipartPartsWithTx(tx *gorm.DB, ctx context.Context, userID int64, uploadID string) error
}
