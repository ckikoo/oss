package async

import (
	"context"
	"time"

	"oss/adaptor"
	"oss/adaptor/repo/model"
	"oss/adaptor/repo/query"
	"oss/service/do"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type IAsyncTaskRepo interface {
	CreateAsyncTask(ctx context.Context, task *do.CreateAsyncTask) (int64, error)
	GetAsyncTaskByID(ctx context.Context, taskID int64) (*do.AsyncTaskDo, error)
	UpdateAsyncTask(ctx context.Context, taskID int64, update *do.UpdateAsyncTask) (*do.AsyncTaskDo, error)
}

type AsyncTaskRepo struct {
	db *gorm.DB
}

func NewAsyncTaskRepo(adaptor adaptor.IAdaptor) IAsyncTaskRepo {
	sqlDB := adaptor.GetDB()
	ormDB, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		panic(err)
	}

	return &AsyncTaskRepo{
		db: ormDB,
	}
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
		return 0, err
	}

	return modelTask.ID, nil
}

func (r *AsyncTaskRepo) GetAsyncTaskByID(ctx context.Context, taskID int64) (*do.AsyncTaskDo, error) {
	q := query.Use(r.db)
	modelTask, err := q.AsyncTask.WithContext(ctx).Where(q.AsyncTask.ID.Eq(taskID)).First()
	if err != nil {
		return nil, err
	}

	return &do.AsyncTaskDo{
		ID:         modelTask.ID,
		UserId:     modelTask.UserID,
		TaskID:     modelTask.TaskID,
		TaskType:   modelTask.TaskType,
		UploadID:   *modelTask.UploadID,
		ObjectID:   *modelTask.ObjectID,
		Status:     modelTask.Status,
		Progress:   modelTask.Progress,
		Result:     *modelTask.Result,
		ErrorMsg:   *modelTask.ErrorMsg,
		RetryCount: modelTask.RetryCount,
		MaxRetry:   modelTask.MaxRetry,
		WorkerID:   *modelTask.WorkerID,
		StartedAt:  *modelTask.StartedAt,
		FinishedAt: *modelTask.FinishedAt,
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
		return nil, err
	}

	// 返回更新后的记录
	return r.GetAsyncTaskByID(ctx, taskID)
}
