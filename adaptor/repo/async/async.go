package async

import (
	"context"
	"time"

	"oss/adaptor/tx"
	"oss/service/do"
)

type IAsyncTaskRepo interface {
	WithTx(tx tx.Tx) IAsyncTaskRepo
	CreateAsyncTask(ctx context.Context, task *do.CreateAsyncTask) (int64, error)
	GetAsyncTaskByID(ctx context.Context, taskID int64) (*do.AsyncTaskDo, error)
	ListRunnableAsyncTasks(ctx context.Context, limit int) ([]*do.AsyncTaskDo, error)
	ClaimAsyncTask(ctx context.Context, taskID int64, workerID string, lockTTL time.Duration) (bool, *do.AsyncTaskDo, error)
	UpdateAsyncTask(ctx context.Context, taskID int64, update *do.UpdateAsyncTask) (*do.AsyncTaskDo, error)
}
