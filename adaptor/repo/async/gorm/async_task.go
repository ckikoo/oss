package gorm

import (
	"context"
	"oss/adaptor/repo/async"
	"oss/adaptor/repo/model"
	"oss/adaptor/repo/query"
	"oss/adaptor/repo/repoerr"
	"oss/adaptor/tx"
	"oss/consts"
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
	now := time.Now()
	maxRetry := task.MaxRetry
	if maxRetry == 0 {
		maxRetry = 3
	}

	modelTask := &model.AsyncTask{
		UserID:     task.UserId,
		TaskType:   task.TaskType,
		BizType:    task.BizType,
		BizID:      task.BizID,
		Status:     task.Status,
		Progress:   task.Progress,
		RetryCount: task.RetryCount,
		MaxRetry:   maxRetry,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if task.Result != "" {
		modelTask.Result = &task.Result
	}
	if task.LastError != "" {
		modelTask.LastError = &task.LastError
	}
	if task.LockedBy != "" {
		modelTask.LockedBy = &task.LockedBy
	}
	if !task.LockedUntil.IsZero() {
		modelTask.LockedUntil = &task.LockedUntil
	}
	if !task.StartedAt.IsZero() {
		modelTask.StartedAt = &task.StartedAt
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

	return toAsyncTaskDo(modelTask), nil
}

func (r *AsyncTaskRepo) ListRunnableAsyncTasks(ctx context.Context, limit int) ([]*do.AsyncTaskDo, error) {
	if limit <= 0 {
		limit = 50
	}

	now := time.Now()
	var modelTasks []*model.AsyncTask
	err := r.db.WithContext(ctx).
		Model(&model.AsyncTask{}).
		Where("status = ? OR (status = ? AND locked_until IS NOT NULL AND locked_until < ?)",
			consts.TaskStatusPending,
			consts.TaskStatusRunning,
			now,
		).
		Order("id ASC").
		Limit(limit).
		Find(&modelTasks).Error
	if err != nil {
		return nil, repoerr.Wrap(err)
	}

	tasks := make([]*do.AsyncTaskDo, 0, len(modelTasks))
	for _, modelTask := range modelTasks {
		tasks = append(tasks, toAsyncTaskDo(modelTask))
	}
	return tasks, nil
}

func (r *AsyncTaskRepo) ClaimAsyncTask(ctx context.Context, taskID int64, workerID string, lockTTL time.Duration) (bool, *do.AsyncTaskDo, error) {
	if workerID == "" {
		workerID = "unknown"
	}
	if lockTTL <= 0 {
		lockTTL = 30 * time.Second
	}

	now := time.Now()
	lockedUntil := now.Add(lockTTL)
	result := r.db.WithContext(ctx).
		Model(&model.AsyncTask{}).
		Where("id = ? AND (status = ? OR (status = ? AND locked_until IS NOT NULL AND locked_until < ?))",
			taskID,
			consts.TaskStatusPending,
			consts.TaskStatusRunning,
			now,
		).
		Updates(map[string]interface{}{
			"status":       consts.TaskStatusRunning,
			"locked_by":    workerID,
			"locked_until": lockedUntil,
			"started_at":   gorm.Expr("COALESCE(started_at, ?)", now),
			"updated_at":   now,
		})
	if result.Error != nil {
		return false, nil, repoerr.Wrap(result.Error)
	}
	if result.RowsAffected == 0 {
		return false, nil, nil
	}

	task, err := r.GetAsyncTaskByID(ctx, taskID)
	if err != nil {
		return true, nil, err
	}
	return true, task, nil
}

func toAsyncTaskDo(modelTask *model.AsyncTask) *do.AsyncTaskDo {
	result := ""
	if modelTask.Result != nil {
		result = *modelTask.Result
	}
	lastError := ""
	if modelTask.LastError != nil {
		lastError = *modelTask.LastError
	}
	lockedBy := ""
	if modelTask.LockedBy != nil {
		lockedBy = *modelTask.LockedBy
	}
	lockedUntil := time.Time{}
	if modelTask.LockedUntil != nil {
		lockedUntil = *modelTask.LockedUntil
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
		ID:          modelTask.ID,
		UserId:      modelTask.UserID,
		TaskType:    modelTask.TaskType,
		BizType:     modelTask.BizType,
		BizID:       modelTask.BizID,
		Status:      modelTask.Status,
		Progress:    modelTask.Progress,
		Result:      result,
		LastError:   lastError,
		RetryCount:  modelTask.RetryCount,
		MaxRetry:    modelTask.MaxRetry,
		LockedBy:    lockedBy,
		LockedUntil: lockedUntil,
		StartedAt:   startedAt,
		FinishedAt:  finishedAt,
		CreatedAt:   modelTask.CreatedAt,
		UpdatedAt:   modelTask.UpdatedAt,
	}
}

func (r *AsyncTaskRepo) UpdateAsyncTask(ctx context.Context, taskID int64, update *do.UpdateAsyncTask) (*do.AsyncTaskDo, error) {
	q := query.Use(r.db).AsyncTask
	now := time.Now()

	updates := make(map[string]interface{})
	if update.Status != 0 {
		updates[q.Status.ColumnName().String()] = update.Status
		if update.Status == consts.TaskStatusCompleted || update.Status == consts.TaskStatusFailed {
			updates[q.LockedBy.ColumnName().String()] = nil
			updates[q.LockedUntil.ColumnName().String()] = nil
			if update.FinishedAt.IsZero() {
				updates[q.FinishedAt.ColumnName().String()] = now
			}
		}
	}
	if update.Progress != 0 {
		updates[q.Progress.ColumnName().String()] = update.Progress
	}
	if update.Result != "" {
		updates[q.Result.ColumnName().String()] = update.Result
	}
	if update.LastError != "" {
		updates[q.LastError.ColumnName().String()] = update.LastError
	}
	if update.RetryCount != 0 {
		updates[q.RetryCount.ColumnName().String()] = update.RetryCount
	}
	if !update.LockedUntil.IsZero() {
		updates[q.LockedUntil.ColumnName().String()] = update.LockedUntil
	}
	if !update.StartedAt.IsZero() {
		updates[q.StartedAt.ColumnName().String()] = update.StartedAt
	}
	if !update.FinishedAt.IsZero() {
		updates[q.FinishedAt.ColumnName().String()] = update.FinishedAt
	}
	updates[q.UpdatedAt.ColumnName().String()] = now

	queryDo := q.WithContext(ctx).Where(q.ID.Eq(taskID))
	if update.LockedBy != "" {
		queryDo = queryDo.Where(q.LockedBy.Eq(update.LockedBy))
	}
	_, err := queryDo.Updates(updates)
	if err != nil {
		return nil, repoerr.Wrap(err)
	}

	return r.GetAsyncTaskByID(ctx, taskID)
}
