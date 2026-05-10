package admin

import (
	"context"
	"oss/adaptor/repo/model"
	"oss/adaptor/repo/query"
	"oss/consts"
	"oss/service/do"
	"time"

	"gorm.io/gorm"
)

type User struct {
	db *gorm.DB
}

var _ IUser = (*User)(nil)

func NewUserRepo(db *gorm.DB) *User {
	return &User{db: db}
}

func (u *User) CreateUser(ctx context.Context, req *do.CreateUser) (int64, error) {
	qs := query.Use(u.db).User

	time := time.Now()

	user := &model.User{
		Email:        req.Email,
		Status:       consts.UserStatusEnable,
		StorageQuota: req.StorageQuota,
		CreatedAt:    time,
		UpdatedAt:    time,
	}

	err := qs.WithContext(ctx).Create(user)
	if err != nil {
		return 0, err
	}

	return user.ID, nil
}
func (u *User) GetUserInfoById(ctx context.Context, id int64) (*do.UserDo, error) {
	qs := query.Use(u.db).User

	uinfo, err := qs.WithContext(ctx).Where(qs.ID.Eq(id)).First()
	if err != nil {
		return nil, err
	}

	return &do.UserDo{
		ID:           uinfo.ID,
		Email:        uinfo.Email,
		Status:       uinfo.Status,
		StorageQuota: uinfo.StorageQuota,
		StorageUsed:  uinfo.StorageUsed,
		CreatedAt:    uinfo.CreatedAt,
		UpdatedAt:    uinfo.UpdatedAt,
	}, nil
}
func (u *User) UpdateStorageUsed(ctx context.Context, id int64, storage int64) error {
	qs := query.Use(u.db).User

	_, err := qs.WithContext(ctx).Where(qs.ID.Eq(id)).UpdateColumns(qs.StorageUsed.Add(storage))
	if err != nil {
		return err
	}

	return nil

}

func (u *User) UpdateStorageUsedWithTx(tx *gorm.DB, ctx context.Context, id int64, storage int64) error {
	qs := query.Use(tx).User

	_, err := qs.WithContext(ctx).Where(qs.ID.Eq(id)).UpdateColumns(qs.StorageUsed.Add(storage))
	if err != nil {
		return err
	}

	return nil
}
