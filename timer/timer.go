package timer

import (
	"context"
	"oss/adaptor"
	"oss/adaptor/repo/async"

	"oss/service/do"
	"oss/utils/logger"
	"strings"
	"time"

	"go.uber.org/zap"
)

var log = logger.GetLogger()

func updateTaskStatus(ctx context.Context, taskRepo async.IAsyncTaskRepo, taskID string, status int32, errMsg string) error {
	update := &do.UpdateAsyncTask{Status: status}
	if errMsg != "" {
		update.ErrorMsg = errMsg
	}
	_, err := taskRepo.UpdateAsyncTask(ctx, taskID, update)
	return err
}

func buildLockKey(keys ...string) string {
	return strings.Join(keys, ":")
}

// 定时器相关的代码
const (
	taskInterval               = 30 * time.Second
	uploadMergeTimeoutInterval = 30 * time.Second
	lifecycleInterval          = 1 * time.Minute
	eventDeliveryInterval      = 10 * time.Second
)

func runTimerTask(ctx context.Context, name string, done chan struct{}, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error("timer handler panic",
					zap.String("handler", name),
					zap.Any("panic", r),
					zap.Stack("stack"))
			}
			select {
			case done <- struct{}{}:
			default:
			}
		}()

		fn()
	}()
}

func startTimerTaskLoop(ctx context.Context, name string, interval time.Duration, done chan struct{}, fn func()) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// 尝试第一次立即执行
		select {
		case <-done:
			runTimerTask(ctx, name, done, fn)
		default:
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				select {
				case <-done:
					runTimerTask(ctx, name, done, fn)
				default:
					// 上一轮还没完成，跳过
				}
			}
		}
	}()
}

func StartTimer(ctx context.Context, adaptor adaptor.IAdaptor) {
	taskDone := make(chan struct{}, 1)
	timeoutDone := make(chan struct{}, 1)
	lifecycleDone := make(chan struct{}, 1)
	scanLifecycleDone := make(chan struct{}, 1)
	eventDone := make(chan struct{}, 1)
	taskDone <- struct{}{}
	timeoutDone <- struct{}{}
	lifecycleDone <- struct{}{}
	scanLifecycleDone <- struct{}{}
	eventDone <- struct{}{}

	startTimerTaskLoop(ctx, "handlerTask", taskInterval, taskDone, func() {
		handlerTask(ctx, adaptor)
	})

	startTimerTaskLoop(ctx, "handlerUploadMergeTimeout", uploadMergeTimeoutInterval, timeoutDone, func() {
		handlerUploadMergeTimeout(ctx, adaptor)
	})
	startTimerTaskLoop(ctx, "handlerLifecycleEvents", lifecycleInterval, lifecycleDone, func() {
		handlerLifecycleEvents(ctx, adaptor)
	})
	startTimerTaskLoop(ctx, "handlerEventDeliveries", eventDeliveryInterval, eventDone, func() {
		handlerEventDeliveries(ctx, adaptor)
	})

	startTimerTaskLoop(ctx, "handlerScanTableLifecycleEvents", lifecycleInterval, scanLifecycleDone, func() {
		handlerScanTableLifecycleEvents(ctx, adaptor)
	})

	<-ctx.Done()
}
