package presigned

import (
	"context"
	"oss/service/do"
)

type IPresignedRepo interface {
	CreatePresignedURL(ctx context.Context, presigned *do.CreatePresignedURL) error
	GetPresignedURLByToken(ctx context.Context, token string) (*do.PresignedURLDo, error)
	DeletePresignedURL(ctx context.Context, token string) error
}
