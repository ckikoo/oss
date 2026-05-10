package admin

import (
	"context"
	"oss/adaptor"
	"oss/service/do"

	"gorm.io/gorm"
)

type IUser interface {
	WithTx(tx *adaptor.Tx) IUser
	CreateUser(ctx context.Context, req *do.CreateUser) (int64, error)
	GetUserInfoById(ctx context.Context, id int64) (*do.UserDo, error)
	UpdateStorageUsed(ctx context.Context, id int64, storage int64) error
	UpdateStorageUsedWithTx(tx *gorm.DB, ctx context.Context, id int64, storage int64) error
}
