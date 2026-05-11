package gorm

import (
	"context"
	"oss/adaptor/repo/async"
	"oss/adaptor/repo/model"
	"oss/adaptor/repo/query"
	"oss/adaptor/repo/repoerr"
	"oss/adaptor/tx"
	"oss/service/do"
	"time"

	"gorm.io/gorm"
)

type AsyncTaskRepo struct {
	db *gorm.DB
}

var _ async.IAsyncTaskRepo = (*AsyncTaskRepo)(nil)

func NewAsyncTaskRepo(db *gorm.DB) async.IAsyncTaskRepo {
	return &AsyncTaskRepo{
		db: db,
	}
}

func (r *AsyncTaskRepo) WithTx(tx tx.Tx) async.IAsyncTaskRepo {
	return &AsyncTaskRepo{db: tx.(*gorm.DB)}
}
func (r *AsyncTaskRepo) CreateAsyncTask(ctx context.Context, task *do.CreateAsyncTask) (int64, error) {
	modelTask := &model.AsyncTask{
		TaskID:     task.TaskID,
		TaskType:   task.TaskType,
		Status:     task.Status,
		Progress:   task.Progress,
		RetryCount: task.RetryCount,
		MaxRetry:   task.MaxRetry,
		StartedAt:  &task.StartedAt,
		UserID:     task.UserId,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	err := r.db.WithContext(ctx).Model(&model.AsyncTask{}).Create(modelTask).Error
	if err != nil {
		return 0, repoerr.Wrap(err)
	}

	return modelTask.ID, nil
}

func (r *AsyncTaskRepo) GetAsyncTaskByID(ctx context.Context, taskID int64) (*do.AsyncTaskDo, error) {
	q := query.Use(r.db)
	modelTask, err := q.AsyncTask.WithContext(ctx).Where(q.AsyncTask.ID.Eq(taskID)).First()
	if err != nil {
		return nil, repoerr.Wrap(err)
	}

	uploadID := ""
	if modelTask.UploadID != nil {
		uploadID = *modelTask.UploadID
	}
	objectID := int64(0)
	if modelTask.ObjectID != nil {
		objectID = *modelTask.ObjectID
	}
	result := ""
	if modelTask.Result != nil {
		result = *modelTask.Result
	}
	errorMsg := ""
	if modelTask.ErrorMsg != nil {
		errorMsg = *modelTask.ErrorMsg
	}
	workerID := ""
	if modelTask.WorkerID != nil {
		workerID = *modelTask.WorkerID
	}
	startedAt := time.Time{}
	if modelTask.StartedAt != nil {
		startedAt = *modelTask.StartedAt
	}
	finishedAt := time.Time{}
	if modelTask.FinishedAt != nil {
		finishedAt = *modelTask.FinishedAt
	}

	return &do.AsyncTaskDo{
		ID:         modelTask.ID,
		UserId:     modelTask.UserID,
		TaskID:     modelTask.TaskID,
		TaskType:   modelTask.TaskType,
		UploadID:   uploadID,
		ObjectID:   objectID,
		Status:     modelTask.Status,
		Progress:   modelTask.Progress,
		Result:     result,
		ErrorMsg:   errorMsg,
		RetryCount: modelTask.RetryCount,
		MaxRetry:   modelTask.MaxRetry,
		WorkerID:   workerID,
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
		CreatedAt:  modelTask.CreatedAt,
		UpdatedAt:  modelTask.UpdatedAt,
	}, nil
}

func (r *AsyncTaskRepo) UpdateAsyncTask(ctx context.Context, taskID int64, update *do.UpdateAsyncTask) (*do.AsyncTaskDo, error) {
	q := query.Use(r.db).AsyncTask

	// 构建更新字段
	updates := make(map[string]interface{})
	if update.Status != 0 {
		updates[q.Status.ColumnName().String()] = update.Status
	}
	if update.Progress != 0 {
		updates[q.Progress.ColumnName().String()] = update.Progress
	}
	if update.Result != "" {
		updates[q.Result.ColumnName().String()] = update.Result
	}
	if update.ErrorMsg != "" {
		updates[q.ErrorMsg.ColumnName().String()] = update.ErrorMsg
	}
	if update.RetryCount != 0 {
		updates[q.RetryCount.ColumnName().String()] = update.RetryCount
	}
	if update.WorkerID != "" {
		updates[q.WorkerID.ColumnName().String()] = update.WorkerID
	}
	if !update.StartedAt.IsZero() {
		updates[q.StartedAt.ColumnName().String()] = update.StartedAt
	}
	if !update.FinishedAt.IsZero() {
		updates[q.FinishedAt.ColumnName().String()] = update.FinishedAt
	}
	updates[q.UpdatedAt.ColumnName().String()] = time.Now()

	// 执行更新
	_, err := q.WithContext(ctx).Where(q.ID.Eq(taskID)).Updates(updates)
	if err != nil {
		return nil, repoerr.Wrap(err)
	}

	// 返回更新后的记录
	return r.GetAsyncTaskByID(ctx, taskID)
}
