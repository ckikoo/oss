package gorm

import (
	"context"
	"oss/adaptor/repo/accesskey"
	"oss/adaptor/repo/model"
	"oss/adaptor/repo/query"
	"oss/adaptor/repo/repoerr"
	"oss/consts"
	"oss/service/do"
	"time"

	"gorm.io/gorm"
)

type AccessKeyRepo struct {
	db *gorm.DB
}

var _ accesskey.IAccessKeyRepo = (*AccessKeyRepo)(nil)

func NewAccessKeyRepo(db *gorm.DB) *AccessKeyRepo {
	return &AccessKeyRepo{db: db}
}

func (r *AccessKeyRepo) toAccessKeyDo(modelAK *model.AccessKey) *do.AccessKeyDo {
	return &do.AccessKeyDo{
		ID:        modelAK.ID,
		UserID:    modelAK.UserID,
		AccessKey: modelAK.AccessKey,
		SecretKey: modelAK.SecretKey,
		Alias: func() string {
			if modelAK.Alias_ != nil {
				return *modelAK.Alias_
			}
			return ""
		}(),
		Status: modelAK.Status,
		Permission: func() string {
			if modelAK.Permission != nil {
				return *modelAK.Permission
			}
			return ""
		}(),
		CreatedAt: modelAK.CreatedAt.UnixMilli(),
		ExpiresAt: func() int64 {
			if modelAK.ExpiresAt != nil {
				return modelAK.ExpiresAt.UnixMilli()
			}
			return 0
		}(),
		LastUsedAt: func() int64 {
			if modelAK.LastUsedAt != nil {
				return modelAK.LastUsedAt.UnixMilli()
			}
			return 0
		}(),
	}
}

func (r *AccessKeyRepo) CreateAccessKey(ctx context.Context, ak *do.CreateAccessKey) (int64, error) {
	modelAK := &model.AccessKey{
		UserID:     ak.UserID,
		AccessKey:  ak.AccessKey,
		SecretKey:  ak.SecretKey,
		Permission: ak.Permission,
		ExpiresAt:  ak.ExpiresAt,
		Status:     consts.AccessKeyStatusEnable,
		CreatedAt:  time.Now(),
	}
	qs := query.Use(r.db).AccessKey.WithContext(ctx)
	err := qs.Create(modelAK)
	if err != nil {
		return 0, repoerr.Wrap(err)
	}
	return modelAK.ID, nil
}

func (r *AccessKeyRepo) GetByAccessKey(ctx context.Context, accessKey string) (*do.AccessKeyDo, error) {
	q := query.Use(r.db)
	qs := q.AccessKey.WithContext(ctx)
	modelAK, err := qs.Where(q.AccessKey.AccessKey.Eq(accessKey)).First()
	if err != nil {
		return nil, repoerr.Wrap(err)
	}
	return r.toAccessKeyDo(modelAK), nil
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
		return nil, repoerr.Wrap(err)
	}
	doAKs := make([]*do.AccessKeyDo, len(modelAKs))
	for i, modelAK := range modelAKs {
		doAKs[i] = r.toAccessKeyDo(modelAK)
	}
	return doAKs, nil
}

func (r *AccessKeyRepo) UpdateStatus(ctx context.Context, accessKey string, status int32) (*do.AccessKeyDo, error) {
	q := query.Use(r.db)
	qs := q.AccessKey.WithContext(ctx)
	_, err := qs.Where(q.AccessKey.AccessKey.Eq(accessKey)).Update(q.AccessKey.Status, status)
	if err != nil {
		return nil, repoerr.Wrap(err)
	}
	return r.GetByAccessKey(ctx, accessKey)
}

func (r *AccessKeyRepo) DeleteAccessKey(ctx context.Context, accessKey string) error {
	q := query.Use(r.db)
	qs := q.AccessKey.WithContext(ctx)
	_, err := qs.Where(q.AccessKey.AccessKey.Eq(accessKey)).Delete()
	return repoerr.Wrap(err)
}
