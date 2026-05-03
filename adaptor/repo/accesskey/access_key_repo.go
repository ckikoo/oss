package accesskey

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

type AccessKeyRepo struct {
	db *gorm.DB
}

var _ IAccessKeyRepo = (*AccessKeyRepo)(nil)

func NewAccessKeyRepo(adaptor adaptor.IAdaptor) *AccessKeyRepo {
	sqlDB := adaptor.GetDB()
	ormDB, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		panic(err)
	}

	return &AccessKeyRepo{db: ormDB}
}

func (r *AccessKeyRepo) CreateAccessKey(ctx context.Context, ak *do.CreateAccessKey) (int64, error) {
	modelAK := &model.AccessKey{
		UserID:     ak.UserID,
		AccessKey:  ak.AccessKey,
		SecretKey:  ak.SecretKey,
		Permission: &ak.Permission,
		ExpiresAt:  ak.ExpiresAt,
		Status:     consts.AccessKeyStatusEnable,
		CreatedAt:  time.Now(),
	}
	qs := query.Use(r.db).AccessKey.WithContext(ctx)
	err := qs.Create(modelAK)
	if err != nil {
		return 0, err
	}
	return modelAK.ID, nil
}

func (r *AccessKeyRepo) GetByAccessKey(ctx context.Context, accessKey string) (*do.AccessKeyDo, error) {
	q := query.Use(r.db)
	qs := q.AccessKey.WithContext(ctx)
	modelAK, err := qs.Where(q.AccessKey.AccessKey.Eq(accessKey)).First()
	if err != nil {
		return nil, err
	}
	doAK := &do.AccessKeyDo{
		ID:         modelAK.ID,
		UserID:     modelAK.UserID,
		AccessKey:  modelAK.AccessKey,
		SecretKey:  modelAK.SecretKey,
		Alias:      *modelAK.Alias_,
		Status:     modelAK.Status,
		Permission: *modelAK.Permission,
		CreatedAt:  modelAK.CreatedAt,
		ExpiresAt:  *modelAK.ExpiresAt,
		LastUsedAt: *modelAK.LastUsedAt,
	}
	return doAK, nil
}

func (r *AccessKeyRepo) CheckAccessKeyAndSecret(ctx context.Context, accessKey string, secretKeyHash string) bool {
	qs := query.Use(r.db).AccessKey

	count, _ := qs.WithContext(ctx).Where(qs.SecretKey.Eq(secretKeyHash), qs.AccessKey.Eq(accessKey)).Count()
	return count > 0
}

func (r *AccessKeyRepo) ListByFilter(ctx context.Context, userID int64, status int32) ([]*do.AccessKeyDo, error) {
	q := query.Use(r.db)
	qs := q.AccessKey.WithContext(ctx)
	if userID > 0 {
		qs = qs.Where(q.AccessKey.UserID.Eq(userID))
	}
	if status != 0 {
		qs = qs.Where(q.AccessKey.Status.Eq(status))
	}
	modelAKs, err := qs.Order(q.AccessKey.ID.Desc()).Find()
	if err != nil {
		return nil, err
	}
	doAKs := make([]*do.AccessKeyDo, len(modelAKs))
	for i, modelAK := range modelAKs {
		doAKs[i] = &do.AccessKeyDo{
			ID:         modelAK.ID,
			UserID:     modelAK.UserID,
			AccessKey:  modelAK.AccessKey,
			SecretKey:  modelAK.SecretKey,
			Alias:      *modelAK.Alias_,
			Status:     modelAK.Status,
			Permission: *modelAK.Permission,
			CreatedAt:  modelAK.CreatedAt,
			ExpiresAt:  *modelAK.ExpiresAt,
			LastUsedAt: *modelAK.LastUsedAt,
		}
	}
	return doAKs, nil
}

func (r *AccessKeyRepo) UpdateStatus(ctx context.Context, accessKey string, status int32) (*do.AccessKeyDo, error) {
	q := query.Use(r.db)
	qs := q.AccessKey.WithContext(ctx)
	_, err := qs.Where(q.AccessKey.AccessKey.Eq(accessKey)).Update(q.AccessKey.Status, status)
	if err != nil {
		return nil, err
	}
	return r.GetByAccessKey(ctx, accessKey)
}

func (r *AccessKeyRepo) DeleteAccessKey(ctx context.Context, accessKey string) error {
	q := query.Use(r.db)
	qs := q.AccessKey.WithContext(ctx)
	_, err := qs.Where(q.AccessKey.AccessKey.Eq(accessKey)).Delete()
	return err
}
