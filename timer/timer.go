package timer

import (
	"context"
	"oss/adaptor"
	"oss/adaptor/redis"
	"oss/adaptor/repo/admin"
	"oss/adaptor/repo/async"
	"oss/adaptor/repo/multipart"
	"oss/adaptor/repo/object"
	"oss/consts"
	"oss/service/do"
	"oss/utils/pool"
	"time"

	"github.com/gogf/gf/util/gconv"
	"github.com/google/uuid"
	"github.com/samber/lo"
)

func updateTaskStatus(ctx context.Context, taskRepo async.IAsyncTaskRepo, taskID int64, status int32, errMsg string) {
	update := &do.UpdateAsyncTask{Status: status}
	if errMsg != "" {
		update.ErrorMsg = errMsg
	}
	_, _ = taskRepo.UpdateAsyncTask(ctx, taskID, update)
}

func handlerTask(ctx context.Context, adaptor adaptor.IAdaptor) {
	// 从 Redis 队列中取出任务 ID
	redisTask := redis.NewTask(adaptor)
	taskRepo := async.NewAsyncTaskRepo(adaptor)
	storage := adaptor.GetStorage()
	multipart := multipart.NewObjectRepo(adaptor)
	fileRepo := object.NewObjectRepo(adaptor)
	taskLocker := redis.NewLock(adaptor)
	uinfoRepo := admin.NewUserRepo(adaptor)
	taskIDs, err := redisTask.DequeueTask(ctx, 50, time.Second*5)

	if err != nil {
		// 处理错误（如日志记录、监控告警等）
		return
	}

	p := pool.NewPoolWithSize(5)

	for _, taskID := range taskIDs {
		taskID := taskID // 每次迭代创建新变量

		p.RunGo(func() {
			currentUUid := uuid.NewString()

			ok, err := taskLocker.AcquireLock(ctx, taskID, currentUUid, time.Second*30)
			if err != nil {
				// 处理错误
				return
			}
			if !ok {
				// 锁被占用，跳过
				return
			}

			defer func() {
				if err := taskLocker.ReleaseLock(ctx, taskID, currentUUid); err != nil {
					// 处理释放锁时的错误（如日志记录、监控告警等）
				}
			}()

			taskCtx, cancelTask := context.WithCancel(ctx)
			defer cancelTask()

			watchCtx, stopWatchdog := context.WithCancel(ctx)
			defer stopWatchdog() // goroutine 随任务结束而退出

			go func() {
				ticker := time.NewTicker(time.Second * 10) // 每 10 秒续期一次
				defer ticker.Stop()
				for {
					select {
					case <-watchCtx.Done():
						return
					case <-ticker.C:
						if err := taskLocker.RefreshLock(ctx, taskID, currentUUid, time.Second*30); err != nil {
							// 续期失败说明锁已丢失，应中断任务
							cancelTask()
							return
						}
					}
				}
			}()

			// 根据 taskID 查询数据库获取任务详情
			task, err := taskRepo.GetAsyncTaskByID(taskCtx, gconv.Int64(taskID))
			if err != nil {
				// 处理错误（如日志记录、监控告警等）
				return
			}

			switch task.TaskType {
			case consts.TaskTypePhysicalMerge:
				info, err := multipart.GetMultipartUploadByID(taskCtx, task.UserId, task.UploadID)
				if err != nil {
					return
				}

				// 处理物理合并任务
				parts, err := multipart.ListMultipartParts(taskCtx, task.UserId, task.UploadID)
				if err != nil {
					return
				}

				partPaths := make([]string, len(parts))
				for i, part := range parts {
					partPaths[i] = part.StoragePath
				}

				saveInfo, err := storage.MergeParts(taskCtx, info.BucketName, info.ObjectKey, partPaths)
				if err != nil {
					writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					updateTaskStatus(writeCtx, taskRepo, task.ID, consts.TaskStatusFailed, err.Error())
					return
				}

				updateTaskStatus(taskCtx, taskRepo, task.ID, consts.TaskStatusCompleted, "")
				fileRepo.UpdateObject(taskCtx, info.BucketName, info.ObjectKey, "", &do.UpdateObject{
					Size:        &saveInfo.Size,
					Etag:        &saveInfo.Etag,
					StoragePath: &saveInfo.StoragePath,
				})

			case consts.TaskTypeAbortMultipart:
				info, err := multipart.GetMultipartUploadByID(taskCtx, task.UserId, task.UploadID)
				if err != nil {
					return
				}

				parts, err := multipart.ListMultipartParts(taskCtx, info.UserID, info.UploadID)
				if err != nil {
					return
				}

				total := 0

				lo.ForEach(parts, func(part *do.MultipartPartDo, _ int) {
					total += int(part.Size)
				})

				multipart.DeleteMultipartParts(taskCtx, task.UserId, task.UploadID)
				uinfoRepo.UpdateStorageUsed(taskCtx, info.UserID, -int64(total))

				storage.DeleteParts(taskCtx, info.BucketName, task.UploadID)

			default:
				// 处理未知任务类型
			}

		})
	}

	p.Wait()
}

// GetTimeWaitMultipartCancel 获取等待取消的 multipart 上传 ID 列表
func handlerUploadMergeTimeout(ctx context.Context, adaptor adaptor.IAdaptor) {
	// 处理上传超时的任务
	multipartRedis := redis.NewMultipart(adaptor)
	multipartRepo := multipart.NewObjectRepo(adaptor)
	storage := adaptor.GetStorage()
	uinfoRepo := admin.NewUserRepo(adaptor)

	list, err := multipartRedis.GetTimeWaitMultipartCancel(ctx)
	if err != nil {
		// 处理错误（如日志记录、监控告警等）
		return
	}

	pool := pool.NewPoolWithSize(2)

	for _, item := range list {
		_ = item // 每次迭代创建新变量，避免闭包捕获问题
		pool.RunGo(func() {

			uploadInfo, err := multipartRepo.GetMultipartUploadByID(ctx, 0, item)
			if err != nil {
				// 处理错误
				return
			}

			// 只有文件已经虚拟合并，证明全部校验完毕，不做任何处理
			if uploadInfo.Status == consts.MultipartPartStatusVirtualMerge {
				return
			}

			total := 0
			parts, err := multipartRepo.ListMultipartParts(ctx, uploadInfo.UserID, uploadInfo.UploadID)
			if err != nil {
				// 处理错误
				return
			}

			lo.ForEach(parts, func(item *do.MultipartPartDo, _ int) {
				total += int(item.Size)
			})

			// 本次存储删除
			storage.DeleteParts(ctx, uploadInfo.BucketName, uploadInfo.UploadID)
			// 用户状态修正
			uinfoRepo.UpdateStorageUsed(ctx, uploadInfo.UserID, -int64(total))
			// 清楚系统文件
			multipartRepo.DeleteMultipartParts(ctx, uploadInfo.UserID, uploadInfo.UploadID)

		})

	}
}

// 定时器相关的代码
func StartTimer(ctx context.Context, adaptor adaptor.IAdaptor) {
	taskDone := make(chan struct{}, 1)
	timeoutDone := make(chan struct{}, 1)
	taskDone <- struct{}{}
	timeoutDone <- struct{}{}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		select {
		case <-taskDone:
			go func() {
				handlerTask(ctx, adaptor)
				taskDone <- struct{}{}
			}()
		default: // 上一轮还没完成，跳过
		}

		select {
		case <-timeoutDone:
			go func() {
				handlerUploadMergeTimeout(ctx, adaptor)
				timeoutDone <- struct{}{}
			}()
		default:
		}

		time.Sleep(time.Second * 30)
	}
}
