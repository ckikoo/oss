package async

import (
	"oss/adaptor"
	"oss/adaptor/redis"
	asyncRepo "oss/adaptor/repo/async"
	gormAsync "oss/adaptor/repo/async/gorm"
	"oss/common"
	"oss/consts"
	"oss/service/do"
	"oss/service/dto"
	"oss/utils/logger"

	"go.uber.org/zap"
)

type Service struct {
	repo       asyncRepo.IAsyncTaskRepo
	asyncRedis redis.ITask
	logger     *zap.Logger
}

func NewService(adaptor adaptor.IAdaptor) *Service {
	return &Service{
		repo:       gormAsync.NewAsyncTaskRepo(adaptor.GetGORM()),
		asyncRedis: redis.NewTask(adaptor),
		logger:     logger.GetLogger().With(zap.String("module", "async")),
	}
}

func (srv *Service) ListTasks(ctx *common.UserInfoCtx, req *dto.ListAsyncTasksReq) (*dto.ListAsyncTasksResp, common.Errno) {
	limit := normalizeListAsyncTasksLimit(req.Limit)
	filter := &do.ListAsyncTasksFilter{
		UserID:   ctx.UserID,
		TaskType: req.TaskType,
		BizType:  req.BizType,
		BizID:    req.BizID,
		Status:   req.Status,
		MarkerID: req.MarkerID,
		Limit:    limit + 1,
	}

	tasks, err := srv.repo.ListAsyncTasks(ctx, filter)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	truncated := len(tasks) > limit
	if truncated {
		tasks = tasks[:limit]
	}

	items := make([]*dto.AsyncTaskItem, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, toAsyncTaskItem(task))
	}

	resp := &dto.ListAsyncTasksResp{
		Items:       items,
		IsTruncated: truncated,
		Limit:       limit,
	}
	if truncated && len(tasks) > 0 {
		resp.NextMarker = tasks[len(tasks)-1].ID
	}
	return resp, common.OK
}

func (srv *Service) GetTask(ctx *common.UserInfoCtx, taskID int64) (*dto.AsyncTaskResp, common.Errno) {
	task, err := srv.repo.GetAsyncTaskByID(ctx, taskID)
	if err != nil {
		return nil, common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.ResouceNotFoundErr)
	}
	if task.UserId != ctx.UserID {
		return nil, common.AuthErr
	}
	return &dto.AsyncTaskResp{Task: toAsyncTaskItem(task)}, common.OK
}

func (srv *Service) RetryTask(ctx *common.UserInfoCtx, taskID int64) (*dto.AsyncTaskResp, common.Errno) {
	updated, task, err := srv.repo.RetryAsyncTask(ctx, taskID, ctx.UserID)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}
	if !updated || task == nil {
		return nil, common.ParamErr.WithMsg("task is not retryable")
	}

	if queued, err := srv.repo.MarkAsyncTaskQueued(ctx, task.ID); err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	} else if queued {
		if err := srv.asyncRedis.EnqueueTask(ctx, task.ID); err != nil {
			srv.logger.Warn("failed to enqueue retried async task",
				zap.Int64("task_id", task.ID),
				zap.Error(err))
		}
		task, err = srv.repo.GetAsyncTaskByID(ctx, task.ID)
		if err != nil {
			return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
		}
	}

	return &dto.AsyncTaskResp{Task: toAsyncTaskItem(task)}, common.OK
}

func (srv *Service) CancelTask(ctx *common.UserInfoCtx, taskID int64) (*dto.AsyncTaskResp, common.Errno) {
	updated, task, err := srv.repo.CancelAsyncTask(ctx, taskID, ctx.UserID)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}
	if !updated || task == nil {
		return nil, common.ParamErr.WithMsg("task is not cancelable")
	}
	return &dto.AsyncTaskResp{Task: toAsyncTaskItem(task)}, common.OK
}

func normalizeListAsyncTasksLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > 200 {
		return 200
	}
	return limit
}

func toAsyncTaskItem(task *do.AsyncTaskDo) *dto.AsyncTaskItem {
	if task == nil {
		return nil
	}
	duration := task.UpdatedAt.Sub(task.CreatedAt).Milliseconds()
	if duration < 0 {
		duration = 0
	}
	return &dto.AsyncTaskItem{
		ID:            task.ID,
		UserID:        task.UserId,
		TaskType:      task.TaskType,
		BizType:       task.BizType,
		BizID:         task.BizID,
		Status:        task.Status,
		Progress:      task.Progress,
		Result:        task.Result,
		LastError:     task.LastError,
		RetryCount:    task.RetryCount,
		MaxRetry:      task.MaxRetry,
		DurationMs:    duration,
		CreatedAt:     task.CreatedAt.UnixMilli(),
		UpdatedAt:     task.UpdatedAt.UnixMilli(),
		NextRetryable: task.Status == consts.TaskStatusFailed || task.Status == consts.TaskStatusDead,
		Cancelable:    task.Status == consts.TaskStatusPending || task.Status == consts.TaskStatusQueued || task.Status == consts.TaskStatusRunning,
	}
}
