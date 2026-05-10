package multipart

import (
	"context"
	"oss/adaptor/tx"
	"oss/service/do"
)

type IMultipartRepo interface {
	WithTx(tx tx.Tx) IMultipartRepo
	CreateMultipartUpload(ctx context.Context, upload *do.CreateMultipartUpload) (int64, error)
	GetMultipartUploadByID(ctx context.Context, userId int64, uploadID string) (*do.MultipartUploadDo, error)
	UpdateMultipartUpload(ctx context.Context, userID int64, uploadID string, update *do.UpdateMultipartUpload) (*do.MultipartUploadDo, error)
	CreateOrUpdateMultipartPart(ctx context.Context, part *do.CreateMultipartPart) (bool, error)
	GetMultipartPart(ctx context.Context, userID int64, uploadID string, partNumber int32) (*do.MultipartPartDo, error)
	ListMultipartParts(ctx context.Context, userID int64, uploadID string) ([]*do.MultipartPartDo, error)
	DeleteMultipartParts(ctx context.Context, userID int64, uploadID string) error
}
