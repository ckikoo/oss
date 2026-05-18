package timer

import (
	"context"
	"oss/adaptor"
	"oss/adaptor/redis"
	"oss/adaptor/repo/async"
	gormAsync "oss/adaptor/repo/async/gorm"
	"oss/consts"
	"strconv"
	"time"

	"go.uber.org/zap"
)

const queuedStaleAfter = 2 * time.Minute

func handlerScanPendingAsyncTasks(ctx context.Context, adaptor adaptor.IAdaptor) {
	taskRepo := gormAsync.NewAsyncTaskRepo(adaptor.GetGORM())
	redisTask := redis.NewTask(adaptor)
	scanPendingAsyncTasks(ctx, taskRepo, redisTask)
}

func handlerRecoverStaleQueuedAsyncTasks(ctx context.Context, adaptor adaptor.IAdaptor) {
	taskRepo := gormAsync.NewAsyncTaskRepo(adaptor.GetGORM())
	recoverStaleQueuedAsyncTasks(ctx, taskRepo)
}

func handlerRecoverStaleRunningAsyncTasks(ctx context.Context, adaptor adaptor.IAdaptor) {
	taskRepo := gormAsync.NewAsyncTaskRepo(adaptor.GetGORM())
	locker := redis.NewLock(adaptor)
	recoverStaleRunningAsyncTasks(ctx, taskRepo, locker)
}

func scanPendingAsyncTasks(ctx context.Context, taskRepo async.IAsyncTaskRepo, redisTask redis.ITask) {
	tasks, err := taskRepo.QueuePendingAsyncTasks(ctx, 50)
	if err != nil {
		log.Error("timer.scanPendingAsyncTasks fail to scan pending async tasks", zap.Error(err))
		return
	}
	if len(tasks) == 0 {
		return
	}

	taskIDs := make([]int64, 0, len(tasks))
	for _, task := range tasks {
		if task == nil || task.ID <= 0 {
			continue
		}
		taskIDs = append(taskIDs, task.ID)
	}
	if len(taskIDs) == 0 {
		return
	}

	if err := redisTask.EnqueueBatch(ctx, taskIDs); err != nil {
		log.Error("timer.scanPendingAsyncTasks fail to enqueue async tasks", zap.Error(err), zap.Int("count", len(taskIDs)))
	}
}

func recoverStaleQueuedAsyncTasks(ctx context.Context, taskRepo async.IAsyncTaskRepo) {
	tasks, err := taskRepo.ResetStaleQueuedAsyncTasks(ctx, time.Now().Add(-queuedStaleAfter), 50, "redis task queue expired")
	if err != nil {
		log.Error("timer.recoverStaleQueuedAsyncTasks fail to scan queued async tasks", zap.Error(err))
		return
	}
	if len(tasks) > 0 {
		log.Info("timer.recoverStaleQueuedAsyncTasks reset stale queued tasks", zap.Int("count", len(tasks)))
	}
}

func recoverStaleRunningAsyncTasks(ctx context.Context, taskRepo async.IAsyncTaskRepo, locker interface {
	LockExists(context.Context, string) (bool, error)
}) {
	tasks, err := taskRepo.ListRunningAsyncTasks(ctx, 50)
	if err != nil {
		log.Error("timer.recoverStaleRunningAsyncTasks fail to scan running async tasks", zap.Error(err))
		return
	}

	for _, task := range tasks {
		if task == nil || task.ID <= 0 {
			continue
		}
		taskLockKey := buildLockKey(consts.ServerName, "task", strconv.FormatInt(task.ID, 10))
		exists, err := locker.LockExists(ctx, taskLockKey)
		if err != nil {
			log.Error("timer.recoverStaleRunningAsyncTasks fail to check task lock", zap.Error(err), zap.Int64("taskID", task.ID))
			continue
		}
		if exists {
			continue
		}
		if _, err := taskRepo.ResetAsyncTaskToPending(ctx, task.ID, consts.TaskStatusRunning, "redis task lock expired"); err != nil {
			log.Error("timer.recoverStaleRunningAsyncTasks fail to reset task", zap.Error(err), zap.Int64("taskID", task.ID))
		}
	}
}
