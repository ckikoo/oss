package timer

import (
	"context"
	"fmt"
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

const (
	taskInterval               = 30 * time.Second
	taskRecoveryInterval       = 1 * time.Minute
	uploadMergeTimeoutInterval = 30 * time.Second
	lifecycleInterval          = 1 * time.Minute
	eventDeliveryInterval      = 10 * time.Second
)

type Mode string

const (
	ModeAll           Mode = "all"
	ModeAsyncTask     Mode = "task"
	ModeTaskRecovery  Mode = "task-recovery"
	ModeUploadTimeout Mode = "upload-timeout"
	ModeLifecycle     Mode = "lifecycle"
	ModeEventDelivery Mode = "event-delivery"
	ModeScanLifecycle Mode = "scan-lifecycle"
)

var modes = []Mode{
	ModeAll,
	ModeAsyncTask,
	ModeTaskRecovery,
	ModeUploadTimeout,
	ModeLifecycle,
	ModeEventDelivery,
	ModeScanLifecycle,
}

func ValidModes() []string {
	validModes := make([]string, 0, len(modes))
	for _, mode := range modes {
		validModes = append(validModes, string(mode))
	}
	return validModes
}

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
				}
			}
		}
	}()
}

func startTimerHandler(ctx context.Context, name string, interval time.Duration, fn func()) {
	done := make(chan struct{}, 1)
	done <- struct{}{}
	startTimerTaskLoop(ctx, name, interval, done, fn)
}

func StartTimer(ctx context.Context, adaptor adaptor.IAdaptor) {
	if err := StartTimerMode(ctx, adaptor, ModeAll); err != nil {
		log.Error("timer exited with error", zap.Error(err))
	}
}

func StartTimerMode(ctx context.Context, adaptor adaptor.IAdaptor, mode Mode) error {
	switch mode {
	case ModeAll:
		startTimerHandler(ctx, "handlerTask", taskInterval, func() {
			handlerTask(ctx, adaptor)
		})
		startTimerHandler(ctx, "handlerTaskRecovery", taskRecoveryInterval, func() {
			handlerTaskRecovery(ctx, adaptor)
		})
		startTimerHandler(ctx, "handlerUploadMergeTimeout", uploadMergeTimeoutInterval, func() {
			handlerUploadMergeTimeout(ctx, adaptor)
		})
		startTimerHandler(ctx, "handlerLifecycleEvents", lifecycleInterval, func() {
			handlerLifecycleEvents(ctx, adaptor)
		})
		startTimerHandler(ctx, "handlerEventDeliveries", eventDeliveryInterval, func() {
			handlerEventDeliveries(ctx, adaptor)
		})
		startTimerHandler(ctx, "handlerScanTableLifecycleEvents", lifecycleInterval, func() {
			handlerScanTableLifecycleEvents(ctx, adaptor)
		})
	case ModeAsyncTask:
		startTimerHandler(ctx, "handlerTask", taskInterval, func() {
			handlerTask(ctx, adaptor)
		})
	case ModeTaskRecovery:
		startTimerHandler(ctx, "handlerTaskRecovery", taskRecoveryInterval, func() {
			handlerTaskRecovery(ctx, adaptor)
		})
	case ModeUploadTimeout:
		startTimerHandler(ctx, "handlerUploadMergeTimeout", uploadMergeTimeoutInterval, func() {
			handlerUploadMergeTimeout(ctx, adaptor)
		})
	case ModeLifecycle:
		startTimerHandler(ctx, "handlerLifecycleEvents", lifecycleInterval, func() {
			handlerLifecycleEvents(ctx, adaptor)
		})
	case ModeEventDelivery:
		startTimerHandler(ctx, "handlerEventDeliveries", eventDeliveryInterval, func() {
			handlerEventDeliveries(ctx, adaptor)
		})
	case ModeScanLifecycle:
		startTimerHandler(ctx, "handlerScanTableLifecycleEvents", lifecycleInterval, func() {
			handlerScanTableLifecycleEvents(ctx, adaptor)
		})
	default:
		return fmt.Errorf("unsupported timer mode %q, valid modes: %s", mode, strings.Join(ValidModes(), ", "))
	}

	log.Info("timer started", zap.String("mode", string(mode)))
	<-ctx.Done()
	return nil
}
