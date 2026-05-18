package timer

import (
	"context"
	"oss/adaptor"
	"oss/adaptor/redis"
	gormAsync "oss/adaptor/repo/async/gorm"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func handlerTaskRecovery(ctx context.Context, adaptor adaptor.IAdaptor) {
	taskRepo := gormAsync.NewAsyncTaskRepo(adaptor.GetGORM())
	redisTask := redis.NewTask(adaptor)
	locker := redis.NewLock(adaptor)

	lockKey := buildLockKey("task", "pending", "recovery")
	lockID := uuid.NewString()
	ok, err := locker.AcquireLock(ctx, lockKey, lockID, 55*time.Second)
	if err != nil {
		log.Error("timer.handlerTaskRecovery fail to acquire lock", zap.Error(err), zap.String("lockKey", lockKey))
		return
	}
	if !ok {
		return
	}
	defer func() {
		if err := locker.ReleaseLock(ctx, lockKey, lockID); err != nil {
			log.Error("timer.handlerTaskRecovery fail to release lock", zap.Error(err), zap.String("lockKey", lockKey))
		}
	}()

	tasks, err := taskRepo.ListRunnableAsyncTasks(ctx, 100)
	if err != nil {
		log.Error("timer.handlerTaskRecovery fail to scan runnable async tasks", zap.Error(err))
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
		log.Error("timer.handlerTaskRecovery fail to enqueue pending async tasks", zap.Error(err), zap.Int("count", len(taskIDs)))
	}
}
