package gorm

import (
	"context"

	"oss/adaptor/repo/audit"
	"oss/adaptor/repo/model"
	"oss/adaptor/repo/query"
	"oss/adaptor/repo/repoerr"
	"oss/adaptor/tx"
	"oss/service/do"

	"gorm.io/gorm"
)

type OperationLogRepo struct {
	db *gorm.DB
	q  *query.Query
}

var _ audit.IOperationLogRepo = (*OperationLogRepo)(nil)

func NewOperationLogRepo(db *gorm.DB) *OperationLogRepo {
	return &OperationLogRepo{db: db, q: query.Use(db)}
}

func (r *OperationLogRepo) WithTx(tx tx.Tx) audit.IOperationLogRepo {
	return &OperationLogRepo{db: tx.(*gorm.DB), q: query.Use(tx.(*gorm.DB))}
}
func (r *OperationLogRepo) ListByFilter(ctx context.Context, filter *do.OperationLogFilter) ([]*do.OperationLogDo, int64, error) {
	ql := r.q.OperationLog
	q := ql.WithContext(ctx)
	if filter.UserID > 0 {
		q = q.Where(ql.UserID.Eq(filter.UserID))
	}
	if filter.BucketName != "" {
		q = q.Where(ql.BucketName.Eq(filter.BucketName))
	}
	if filter.Action != "" {
		q = q.Where(ql.Action.Eq(filter.Action))
	}
	if filter.Status != nil {
		q = q.Where(ql.Result.Eq(*filter.Status))
	}
	if filter.DateFrom != nil {
		q = q.Where(ql.CreatedAt.Gte(*filter.DateFrom))
	}
	if filter.DateTo != nil {
		q = q.Where(ql.CreatedAt.Lte(*filter.DateTo))
	}

	logs, total, err := q.Order(ql.CreatedAt.Desc()).FindByPage(filter.Pager.GetOffset(), filter.Pager.Limit)
	if err != nil {
		return nil, 0, repoerr.Wrap(err)
	}

	result := make([]*do.OperationLogDo, 0, len(logs))
	for _, row := range logs {
		result = append(result, &do.OperationLogDo{
			ID:        row.ID,
			RequestID: row.RequestID,
			UserID:    row.UserID,
			Action:    row.Action,
			Result:    row.Result,
			ClientIP:  row.ClientIP,
			Duration:  row.DurationMs,
			CreatedAt: row.CreatedAt,
		})
	}

	return result, int64(total), nil
}

func (r *OperationLogRepo) CreateOperationLog(ctx context.Context, operation *do.CreateOperationLog) error {
	modelLog := &model.OperationLog{
		RequestID:    operation.RequestID,
		UserID:       operation.UserID,
		AccessKey:    operation.AccessKey,
		BucketID:     operation.BucketID,
		BucketName:   operation.BucketName,
		ObjectKey:    operation.ObjectKey,
		Action:       operation.Action,
		Result:       operation.Result,
		StatusCode:   operation.StatusCode,
		ErrorCode:    operation.ErrorCode,
		ClientIP:     operation.ClientIP,
		UserAgent:    operation.UserAgent,
		RequestSize:  operation.RequestSize,
		ResponseSize: operation.ResponseSize,
		DurationMs:   operation.DurationMs,
	}

	return r.q.OperationLog.WithContext(ctx).Create(modelLog)
}
