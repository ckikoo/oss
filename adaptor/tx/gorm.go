package tx

import (
	"context"

	"gorm.io/gorm"
)

type GormTxManager struct {
	db *gorm.DB
}

var _ ITxManager = (*GormTxManager)(nil)

func NewGormTxManager(db *gorm.DB) ITxManager {
	return &GormTxManager{db: db}
}

func (t *GormTxManager) RunInTx(ctx context.Context, fn func(tx Tx) error) error {
	return t.db.WithContext(ctx).Transaction(func(gormTx *gorm.DB) error {
		return fn(gormTx)
	})
}
