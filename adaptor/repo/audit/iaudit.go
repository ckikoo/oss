package audit

import (
	"context"

	"oss/adaptor/tx"
	"oss/service/do"
)

type IOperationLogRepo interface {
	WithTx(tx tx.Tx) IOperationLogRepo
	ListByFilter(ctx context.Context, filter *do.OperationLogFilter) ([]*do.OperationLogDo, int64, error)
	CreateOperationLog(ctx context.Context, operation *do.CreateOperationLog) error
}
