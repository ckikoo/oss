package audit

import (
	"context"

	"oss/service/do"
)

type IOperationLogRepo interface {
	ListByFilter(ctx context.Context, filter *do.OperationLogFilter) ([]*do.OperationLogDo, int64, error)
	CreateOperationLog(ctx context.Context, operation *do.CreateOperationLog) error
}
