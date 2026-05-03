package admin

import (
	"context"
	"oss/service/do"
)

type IUser interface {
	CreateUser(ctx context.Context, req *do.CreateUser) (int64, error)
	GetUserInfoById(ctx context.Context, id int64) (*do.UserDo, error)
}
