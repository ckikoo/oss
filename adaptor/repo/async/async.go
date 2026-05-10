package async

import (
	"context"

	"oss/service/do"
)

type IAsyncTaskRepo interface {
	CreateAsyncTask(ctx context.Context, task *do.CreateAsyncTask) (int64, error)
	GetAsyncTaskByID(ctx context.Context, taskID int64) (*do.AsyncTaskDo, error)
	UpdateAsyncTask(ctx context.Context, taskID int64, update *do.UpdateAsyncTask) (*do.AsyncTaskDo, error)
}
