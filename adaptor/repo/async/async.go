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
	ListAsyncTasks(ctx context.Context, filter *do.ListAsyncTasksFilter) ([]*do.AsyncTaskDo, error)
	QueuePendingAsyncTasks(ctx context.Context, limit int) ([]*do.AsyncTaskDo, error)
	ResetStaleQueuedAsyncTasks(ctx context.Context, before time.Time, limit int, errMsg string) ([]*do.AsyncTaskDo, error)
	ListRunningAsyncTasks(ctx context.Context, limit int) ([]*do.AsyncTaskDo, error)
	MarkAsyncTaskQueued(ctx context.Context, taskID int64) (bool, error)
	// 乐观锁， 只有当任务当前状态与预期状态一致时才会更新成功
	ClaimAsyncTask(ctx context.Context, taskID int64) (bool, *do.AsyncTaskDo, error)
	ResetAsyncTaskToPending(ctx context.Context, taskID int64, currentStatus int32, errMsg string) (bool, error)
	CompleteAsyncTask(ctx context.Context, taskID int64, result string) (bool, error)
	FailAsyncTask(ctx context.Context, taskID int64, errMsg string) (bool, *do.AsyncTaskDo, error)
	UpdateAsyncTask(ctx context.Context, taskID int64, update *do.UpdateAsyncTask) (*do.AsyncTaskDo, error)
	RetryAsyncTask(ctx context.Context, taskID int64, userID int64) (bool, *do.AsyncTaskDo, error)
	CancelAsyncTask(ctx context.Context, taskID int64, userID int64) (bool, *do.AsyncTaskDo, error)
	FailAsyncTasksByBiz(ctx context.Context, userID int64, taskType string, bizType string, bizIDs []string, errMsg string) (int64, error)
	DeleteAsyncTasksByBiz(ctx context.Context, userID int64, taskType string, bizType string, bizID string) (int64, error)
}
