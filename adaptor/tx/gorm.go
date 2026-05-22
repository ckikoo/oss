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

func (t *GormTxManager) RunInTx(ctx context.Context, fn func(context.Context, Tx) error) error {
	state := &afterCommitState{}
	txCtx := context.WithValue(ctx, afterCommitKey{}, state)

	err := t.db.WithContext(txCtx).Transaction(func(gormTx *gorm.DB) error {
		return fn(txCtx, gormTx)
	})
	if err != nil {
		return err
	}

	state.run(context.WithoutCancel(txCtx))
	return nil
}
