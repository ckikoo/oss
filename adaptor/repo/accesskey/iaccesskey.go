package accesskey

import (
	"context"
	"oss/service/do"
)

type IAccessKeyRepo interface {
	CreateAccessKey(ctx context.Context, ak *do.CreateAccessKey) (int64, error)
	GetByAccessKey(ctx context.Context, accessKey string) (*do.AccessKeyDo, error)
	CheckAccessKeyAndSecret(ctx context.Context, accessKey string, secretKeyHash string) bool
	ListByFilter(ctx context.Context, userID int64, status int32) ([]*do.AccessKeyDo, error)
	UpdateStatus(ctx context.Context, accessKey string, status int32) (*do.AccessKeyDo, error)
	DeleteAccessKey(ctx context.Context, accessKey string) error
}
