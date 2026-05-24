package gorm

import (
	"context"
	"errors"
	"oss/adaptor/repo/async"
	"oss/adaptor/repo/model"
	"oss/adaptor/repo/query"
	"oss/adaptor/repo/repoerr"
	"oss/adaptor/tx"
	"oss/consts"
	"oss/service/do"
	"time"

	"github.com/samber/lo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AsyncTaskRepo struct {
	db *gorm.DB
	q  *query.Query
}

var _ async.IAsyncTaskRepo = (*AsyncTaskRepo)(nil)

func NewAsyncTaskRepo(db *gorm.DB) async.IAsyncTaskRepo {
	return &AsyncTaskRepo{
		db: db,
		q:  query.Use(db),
	}
}

func (r *AsyncTaskRepo) WithTx(tx tx.Tx) async.IAsyncTaskRepo {
	txDB, _ := tx.(*gorm.DB)
	return &AsyncTaskRepo{
		db: txDB,
		q:  query.Use(txDB),
	}
}

func (r *AsyncTaskRepo) CreateAsyncTask(ctx context.Context, task *do.CreateAsyncTask) (int64, error) {
	if task == nil {
		return 0, repoerr.Wrap(gorm.ErrInvalidData)
	}

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

	err := r.q.AsyncTask.WithContext(ctx).Create(modelTask)
	if err != nil {
		wrapped := repoerr.Wrap(err)
		if errors.Is(wrapped, repoerr.ErrDuplicate) {
			existing, getErr := r.getAsyncTaskByBiz(ctx, task.TaskType, task.BizID)
			if getErr != nil {
				return 0, getErr
			}
			return existing.ID, nil
		}
		return 0, wrapped
	}

	return modelTask.ID, nil
}

func (r *AsyncTaskRepo) GetAsyncTaskByID(ctx context.Context, taskID int64) (*do.AsyncTaskDo, error) {
	q := r.q
	modelTask, err := q.AsyncTask.WithContext(ctx).Where(q.AsyncTask.ID.Eq(taskID)).First()
	if err != nil {
		return nil, repoerr.Wrap(err)
	}

	return toAsyncTaskDo(modelTask), nil
}

func (r *AsyncTaskRepo) QueuePendingAsyncTasks(ctx context.Context, limit int) ([]*do.AsyncTaskDo, error) {
	limit = normalizeAsyncTaskLimit(limit)
	var modelTasks []*model.AsyncTask
	now := time.Now()

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(skipLockedForUpdate()).
			Where("status = ?", consts.TaskStatusPending).
			Order("id ASC").
			Limit(limit).
			Find(&modelTasks).Error; err != nil {
			return err
		}
		if len(modelTasks) == 0 {
			return nil
		}

		if err := tx.Model(&model.AsyncTask{}).
			Where("id IN ?", asyncTaskIDs(modelTasks)).
			Updates(map[string]interface{}{
				"status":     consts.TaskStatusQueued,
				"updated_at": now,
			}).Error; err != nil {
			return err
		}

		for _, task := range modelTasks {
			task.Status = consts.TaskStatusQueued
			task.UpdatedAt = now
		}
		return nil
	})
	if err != nil {
		return nil, repoerr.Wrap(err)
	}
	return toAsyncTaskDos(modelTasks), nil
}

func (r *AsyncTaskRepo) ResetStaleQueuedAsyncTasks(ctx context.Context, before time.Time, limit int, errMsg string) ([]*do.AsyncTaskDo, error) {
	limit = normalizeAsyncTaskLimit(limit)
	if before.IsZero() {
		before = time.Now().Add(-2 * time.Minute)
	}
	if errMsg == "" {
		errMsg = "redis task queue expired"
	}

	var modelTasks []*model.AsyncTask
	now := time.Now()
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(skipLockedForUpdate()).
			Where("status = ? AND updated_at < ?", consts.TaskStatusQueued, before).
			Order("updated_at ASC, id ASC").
			Limit(limit).
			Find(&modelTasks).Error; err != nil {
			return err
		}
		if len(modelTasks) == 0 {
			return nil
		}

		if err := tx.Model(&model.AsyncTask{}).
			Where("id IN ?", asyncTaskIDs(modelTasks)).
			Updates(map[string]interface{}{
				"status":     consts.TaskStatusPending,
				"last_error": errMsg,
				"updated_at": now,
			}).Error; err != nil {
			return err
		}

		for _, task := range modelTasks {
			task.Status = consts.TaskStatusPending
			task.LastError = &errMsg
			task.UpdatedAt = now
		}
		return nil
	})
	if err != nil {
		return nil, repoerr.Wrap(err)
	}
	return toAsyncTaskDos(modelTasks), nil
}

func (r *AsyncTaskRepo) ListRunningAsyncTasks(ctx context.Context, limit int) ([]*do.AsyncTaskDo, error) {
	return r.listAsyncTasksByStatusWithSkipLocked(ctx, consts.TaskStatusRunning, limit)
}

func (r *AsyncTaskRepo) MarkAsyncTaskQueued(ctx context.Context, taskID int64) (bool, error) {
	return r.updateStatus(ctx, taskID, consts.TaskStatusPending, map[string]interface{}{
		"status":     consts.TaskStatusQueued,
		"updated_at": time.Now(),
	})
}

func (r *AsyncTaskRepo) ClaimAsyncTask(ctx context.Context, taskID int64) (bool, *do.AsyncTaskDo, error) {
	var modelTask *model.AsyncTask
	now := time.Now()
	claimed := false

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task model.AsyncTask
		err := tx.WithContext(ctx).
			Clauses(skipLockedForUpdate()).
			Where("id = ?", taskID).
			Take(&task).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if task.Status != consts.TaskStatusQueued {
			return nil
		}

		result := tx.Model(&model.AsyncTask{}).
			Where("id = ? AND status = ?", taskID, consts.TaskStatusQueued).
			Updates(map[string]interface{}{
				"status":     consts.TaskStatusRunning,
				"updated_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}

		task.Status = consts.TaskStatusRunning
		task.UpdatedAt = now
		modelTask = &task
		claimed = true
		return nil
	})
	if err != nil {
		return false, nil, repoerr.Wrap(err)
	}
	if !claimed || modelTask == nil {
		return false, nil, nil
	}
	return true, toAsyncTaskDo(modelTask), nil
}

func (r *AsyncTaskRepo) ResetAsyncTaskToPending(ctx context.Context, taskID int64, currentStatus int32, errMsg string) (bool, error) {
	updates := map[string]interface{}{
		"status":     consts.TaskStatusPending,
		"updated_at": time.Now(),
	}
	if errMsg != "" {
		updates["last_error"] = errMsg
	}
	return r.updateStatus(ctx, taskID, currentStatus, updates)
}

func (r *AsyncTaskRepo) CompleteAsyncTask(ctx context.Context, taskID int64, result string) (bool, error) {
	updates := map[string]interface{}{
		"status":     consts.TaskStatusCompleted,
		"updated_at": time.Now(),
	}
	if result != "" {
		updates["result"] = result
	}
	return r.updateStatus(ctx, taskID, consts.TaskStatusRunning, updates)
}

func (r *AsyncTaskRepo) FailAsyncTask(ctx context.Context, taskID int64, errMsg string) (bool, *do.AsyncTaskDo, error) {
	now := time.Now()
	if errMsg == "" {
		errMsg = "async task failed"
	}

	updated := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		locked, err := lockAsyncTaskByID(ctx, tx, taskID, consts.TaskStatusRunning)
		if err != nil || !locked {
			return err
		}

		result := tx.Model(&model.AsyncTask{}).
			Where("id = ?", taskID).
			Updates(map[string]interface{}{
				"retry_count": gorm.Expr("retry_count + 1"),
				"status": gorm.Expr(
					"CASE WHEN retry_count + 1 < max_retry THEN ? ELSE ? END",
					consts.TaskStatusPending,
					consts.TaskStatusFailed,
				),
				"last_error": errMsg,
				"updated_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		updated = result.RowsAffected > 0
		return nil
	})
	if err != nil {
		return false, nil, repoerr.Wrap(err)
	}
	if !updated {
		return false, nil, nil
	}

	task, err := r.GetAsyncTaskByID(ctx, taskID)
	if err != nil {
		return true, nil, err
	}
	return true, task, nil
}

func (r *AsyncTaskRepo) UpdateAsyncTask(ctx context.Context, taskID int64, update *do.UpdateAsyncTask) (*do.AsyncTaskDo, error) {
	if update == nil {
		return nil, repoerr.Wrap(gorm.ErrInvalidData)
	}

	updates := make(map[string]interface{})
	if update.Status != 0 {
		updates["status"] = update.Status
	}
	if update.Progress != 0 {
		updates["progress"] = update.Progress
	}
	if update.Result != "" {
		updates["result"] = update.Result
	}
	if update.LastError != "" {
		updates["last_error"] = update.LastError
	}
	if update.RetryCount != 0 {
		updates["retry_count"] = update.RetryCount
	}
	updates["updated_at"] = time.Now()

	err := r.db.WithContext(ctx).
		Model(&model.AsyncTask{}).
		Where("id = ?", taskID).
		Where("status IN ?", []int32{consts.TaskStatusPending, consts.TaskStatusQueued, consts.TaskStatusRunning}).
		Updates(updates).Error
	if err != nil {
		return nil, repoerr.Wrap(err)
	}

	return r.GetAsyncTaskByID(ctx, taskID)
}

func (r *AsyncTaskRepo) FailAsyncTasksByBiz(ctx context.Context, userID int64, taskType string, bizType string, bizIDs []string, errMsg string) (int64, error) {
	if taskType == "" || bizType == "" {
		return 0, repoerr.Wrap(gorm.ErrInvalidData)
	}
	bizIDs = lo.Compact(bizIDs)
	if len(bizIDs) == 0 {
		return 0, nil
	}
	if errMsg == "" {
		errMsg = "async task failed"
	}

	db := r.db.WithContext(ctx).
		Model(&model.AsyncTask{}).
		Where("task_type = ? AND biz_type = ? AND biz_id IN ?", taskType, bizType, bizIDs).
		Where("status IN ?", []int32{consts.TaskStatusPending, consts.TaskStatusQueued, consts.TaskStatusRunning})
	if userID > 0 {
		db = db.Where("user_id = ?", userID)
	}

	result := db.Updates(map[string]interface{}{
		"status":     consts.TaskStatusFailed,
		"last_error": errMsg,
		"updated_at": time.Now(),
	})
	if result.Error != nil {
		return 0, repoerr.Wrap(result.Error)
	}
	return result.RowsAffected, nil
}

func (r *AsyncTaskRepo) DeleteAsyncTasksByBiz(ctx context.Context, userID int64, taskType string, bizType string, bizID string) (int64, error) {
	if taskType == "" || bizType == "" || bizID == "" {
		return 0, repoerr.Wrap(gorm.ErrInvalidData)
	}

	db := r.db.WithContext(ctx).
		Where("task_type = ? AND biz_type = ? AND biz_id = ?", taskType, bizType, bizID)
	if userID > 0 {
		db = db.Where("user_id = ?", userID)
	}

	result := db.Delete(&model.AsyncTask{})
	if result.Error != nil {
		return 0, repoerr.Wrap(result.Error)
	}
	return result.RowsAffected, nil
}

func (r *AsyncTaskRepo) getAsyncTaskByBiz(ctx context.Context, taskType string, bizID string) (*do.AsyncTaskDo, error) {
	q := r.q.AsyncTask
	modelTask, err := q.WithContext(ctx).
		Where(q.TaskType.Eq(taskType), q.BizID.Eq(bizID)).
		First()
	if err != nil {
		return nil, repoerr.Wrap(err)
	}
	return toAsyncTaskDo(modelTask), nil
}

func (r *AsyncTaskRepo) listAsyncTasksByStatusWithSkipLocked(ctx context.Context, status int32, limit int) ([]*do.AsyncTaskDo, error) {
	limit = normalizeAsyncTaskLimit(limit)

	var modelTasks []*model.AsyncTask
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Clauses(skipLockedForUpdate()).
			Where("status = ?", status).
			Order("id ASC").
			Limit(limit).
			Find(&modelTasks).Error
	})
	if err != nil {
		return nil, repoerr.Wrap(err)
	}

	return toAsyncTaskDos(modelTasks), nil
}

func (r *AsyncTaskRepo) updateStatus(ctx context.Context, taskID int64, currentStatus int32, updates map[string]interface{}) (bool, error) {
	updated := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		locked, err := lockAsyncTaskByID(ctx, tx, taskID, currentStatus)
		if err != nil || !locked {
			return err
		}

		result := tx.Model(&model.AsyncTask{}).
			Where("id = ?", taskID).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		updated = result.RowsAffected > 0
		return nil
	})
	if err != nil {
		return false, repoerr.Wrap(err)
	}
	return updated, nil
}

func skipLockedForUpdate() clause.Locking {
	return clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}
}

func lockAsyncTaskByID(ctx context.Context, tx *gorm.DB, taskID int64, status int32) (bool, error) {
	var task model.AsyncTask
	err := tx.WithContext(ctx).
		Clauses(skipLockedForUpdate()).
		Select("id").
		Where("id = ? AND status = ?", taskID, status).
		Take(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func normalizeAsyncTaskLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	return limit
}

func asyncTaskIDs(modelTasks []*model.AsyncTask) []int64 {
	ids := make([]int64, 0, len(modelTasks))
	for _, task := range modelTasks {
		if task != nil && task.ID > 0 {
			ids = append(ids, task.ID)
		}
	}
	return ids
}

func toAsyncTaskDos(modelTasks []*model.AsyncTask) []*do.AsyncTaskDo {
	tasks := make([]*do.AsyncTaskDo, 0, len(modelTasks))
	for _, modelTask := range modelTasks {
		tasks = append(tasks, toAsyncTaskDo(modelTask))
	}
	return tasks
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

	return &do.AsyncTaskDo{
		ID:         modelTask.ID,
		UserId:     modelTask.UserID,
		TaskType:   modelTask.TaskType,
		BizType:    modelTask.BizType,
		BizID:      modelTask.BizID,
		Status:     modelTask.Status,
		Progress:   modelTask.Progress,
		Result:     result,
		LastError:  lastError,
		RetryCount: modelTask.RetryCount,
		MaxRetry:   modelTask.MaxRetry,
		CreatedAt:  modelTask.CreatedAt,
		UpdatedAt:  modelTask.UpdatedAt,
	}
}
