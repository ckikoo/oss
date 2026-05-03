package presigned

import (
	"context"
	"errors"
	"time"

	"oss/adaptor"
	"oss/adaptor/repo/model"
	"oss/adaptor/repo/query"
	"oss/service/do"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type PresignedRepo struct {
	db *gorm.DB
}

var _ IPresignedRepo = (*PresignedRepo)(nil)

func NewPresignedRepo(adaptor adaptor.IAdaptor) *PresignedRepo {
	sqlDB := adaptor.GetDB()
	ormDB, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		panic(err)
	}
	return &PresignedRepo{db: ormDB}
}

func (r *PresignedRepo) CreatePresignedURL(ctx context.Context, presigned *do.CreatePresignedURL) error {
	modelPresigned := &model.PresignedURL{
		Token:         presigned.Token,
		BucketID:      presigned.BucketID,
		ObjectKey:     presigned.ObjectKey,
		ObjectKeyHash: presigned.ObjectKeyHash,
		Method:        presigned.Method,
		UserID:        presigned.UserID,
		SingleUse:     presigned.SingleUse,
		Used:          0,
		ExpiresAt:     presigned.ExpiresAt,
		CreatedAt:     time.Now(),
	}
	return query.Use(r.db).PresignedURL.WithContext(ctx).Create(modelPresigned)
}

func (r *PresignedRepo) GetPresignedURLByToken(ctx context.Context, token string) (*do.PresignedURLDo, error) {
	modelPresigned, err := query.Use(r.db).PresignedURL.WithContext(ctx).Where(query.Use(r.db).PresignedURL.Token.Eq(token)).First()
	if err != nil {
		return nil, err
	}
	if modelPresigned == nil {
		return nil, errors.New("presigned url not found")
	}
	return &do.PresignedURLDo{
		ID:            modelPresigned.ID,
		Token:         modelPresigned.Token,
		BucketID:      modelPresigned.BucketID,
		ObjectKey:     modelPresigned.ObjectKey,
		ObjectKeyHash: modelPresigned.ObjectKeyHash,
		Method:        modelPresigned.Method,
		SingleUse:     modelPresigned.SingleUse,
		Used:          modelPresigned.Used,
		UserID:        modelPresigned.UserID,
		ExpiresAt:     modelPresigned.ExpiresAt,
		CreatedAt:     modelPresigned.CreatedAt,
	}, nil
}

func (r *PresignedRepo) DeletePresignedURL(ctx context.Context, token string) error {
	modelPresigned, err := query.Use(r.db).PresignedURL.WithContext(ctx).Where(query.Use(r.db).PresignedURL.Token.Eq(token)).First()
	if err != nil {
		return err
	}
	if modelPresigned == nil {
		return errors.New("presigned url not found")
	}
	_, err = query.Use(r.db).PresignedURL.WithContext(ctx).Delete(modelPresigned)
	return err
}
