package admin

import (
	"context"
	"oss/adaptor/tx"
	"oss/service/do"
)

type IUser interface {
	WithTx(tx tx.Tx) IUser
	CreateUser(ctx context.Context, req *do.CreateUser) (int64, error)
	GetUserInfoById(ctx context.Context, id int64) (*do.UserDo, error)
	UpdateStorageUsed(ctx context.Context, id int64, storage int64) error
}
