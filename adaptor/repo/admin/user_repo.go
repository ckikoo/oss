package admin

import (
	"context"
	"oss/adaptor"
	"oss/adaptor/repo/model"
	"oss/adaptor/repo/query"
	"oss/consts"
	"oss/service/do"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type User struct {
	db *gorm.DB
}

var _ IUser = (*User)(nil)

func NewUser(adaptor adaptor.IAdaptor) *User {
	sqlDB := adaptor.GetDB()
	ormDB, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		panic(err)
	}

	return &User{db: ormDB}
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
