package async

import (
	"context"

	"oss/adaptor/tx"
	"oss/service/do"
)

type IAsyncTaskRepo interface {
	WithTx(tx tx.Tx) IAsyncTaskRepo
	CreateAsyncTask(ctx context.Context, task *do.CreateAsyncTask) (int64, error)
	GetAsyncTaskByID(ctx context.Context, taskID int64) (*do.AsyncTaskDo, error)
	UpdateAsyncTask(ctx context.Context, taskID int64, update *do.UpdateAsyncTask) (*do.AsyncTaskDo, error)
}
