package multipart

import (
	"context"
	"oss/service/do"
)

type IMultipartRepo interface {
	CreateMultipartUpload(ctx context.Context, upload *do.CreateMultipartUpload) (int64, error)
	GetMultipartUploadByID(ctx context.Context, uploadID string) (*do.MultipartUploadDo, error)
	UpdateMultipartUpload(ctx context.Context, uploadID string, update *do.UpdateMultipartUpload) (*do.MultipartUploadDo, error)
	CreateOrUpdateMultipartPart(ctx context.Context, part *do.CreateMultipartPart) (bool, error)
	GetMultipartPart(ctx context.Context, uploadID string, partNumber int32) (*do.MultipartPartDo, error)
	ListMultipartParts(ctx context.Context, uploadID string) ([]*do.MultipartPartDo, error)
	DeleteMultipartParts(ctx context.Context, uploadID string) error
}
