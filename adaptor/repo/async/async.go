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
	QueuePendingAsyncTasks(ctx context.Context, limit int) ([]*do.AsyncTaskDo, error)
	ResetStaleQueuedAsyncTasks(ctx context.Context, before time.Time, limit int, errMsg string) ([]*do.AsyncTaskDo, error)
	ListRunningAsyncTasks(ctx context.Context, limit int) ([]*do.AsyncTaskDo, error)
	MarkAsyncTaskQueued(ctx context.Context, taskID int64) (bool, error)
	ClaimAsyncTask(ctx context.Context, taskID int64) (bool, *do.AsyncTaskDo, error)
	ResetAsyncTaskToPending(ctx context.Context, taskID int64, currentStatus int32, errMsg string) (bool, error)
	CompleteAsyncTask(ctx context.Context, taskID int64, result string) (bool, error)
	FailAsyncTask(ctx context.Context, taskID int64, errMsg string) (bool, *do.AsyncTaskDo, error)
	UpdateAsyncTask(ctx context.Context, taskID int64, update *do.UpdateAsyncTask) (*do.AsyncTaskDo, error)
}
