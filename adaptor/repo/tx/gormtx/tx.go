package gormtx

import (
	"context"
	"oss/adaptor/tx"

	"gorm.io/gorm"
)

type GormTxManager struct {
	db *gorm.DB
}

var _ tx.ITxManager = (*GormTxManager)(nil)

func NewTxManager(db *gorm.DB) tx.ITxManager {
	return &GormTxManager{db: db}
}

func (t *GormTxManager) RunInTx(ctx context.Context, fn func(tx.Tx) error) error {
	return t.db.WithContext(ctx).Transaction(func(gormTx *gorm.DB) error {
		return fn(gormTx)
	})
}
