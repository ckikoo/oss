package timer

import (
	"context"
	"oss/adaptor"
	"oss/adaptor/redis"
	gormLifecycle "oss/adaptor/repo/lifecycle/gorm"
	gormObject "oss/adaptor/repo/object/gorm"
	"oss/service/do"
	"time"

	"go.uber.org/zap"
)

func handlerScanTableLifecycleEvents(ctx context.Context, adaptor adaptor.IAdaptor) {
	repo := gormLifecycle.NewLifecycleRepo(adaptor.GetGORM())
	objRepo := gormObject.NewObjectRepo(adaptor)

	rds := redis.NewLifecycle(adaptor)

	const batchSize = 100
	var cursor int64 = 0

	// 有可能存在大量的生命周期规则，应该分页扫描，避免一次性 OOM， mvp 阶段先全部
	for {
		rules, err := repo.ListAllActiveLifecycleRulesByCursor(ctx, cursor, batchSize)
		if err != nil {
			log.Error("timer.handlerScanTableLifecycleEvents ListAllActiveLifecycleRules", zap.Error(err))
			return
		}

		for _, rule := range rules {

			prefix := func() string {
				if rule.Prefix == nil {
					return ""
				}
				return *rule.Prefix
			}()

			for {
				// 后续使用游标优化
				offset := 0
				list, err := objRepo.ListByBucketWithPrefix(ctx, &do.ListObjectsByBucket{
					BucketID: rule.BucketID,
					Prefix:   prefix,
					Limit:    batchSize,
					Offset:   offset,
				})

				if err != nil {
					log.Error("timer.handlerScanTableLifecycleEvents ListByBucketWithPrefix failed", zap.Int64("bucketID", rule.BucketID), zap.Int64("ruleID", rule.ID), zap.Error(err))
					continue
				}

				for _, obj := range list {
					if rule.TransitionDays != nil && *rule.TransitionDays > 0 {
						executeTime := obj.CreatedAt.AddDate(0, 0, int(*rule.TransitionDays))
						rds.SetLifecycleEvent(ctx, rule.BucketID, rule.ID, *rule.Prefix, "expiration", obj.ObjectKey, executeTime)
					}

					if rule.TransitionDays != nil && *rule.TransitionDays > 0 {
						rds.SetLifecycleEvent(ctx, rule.BucketID, rule.ID, *rule.Prefix, "transition", obj.ObjectKey, obj.CreatedAt.AddDate(0, 0, int(*rule.TransitionDays)))
					}
				}

				offset += len(list)

				time.Sleep(time.Millisecond * 200) // 让出 CPU，避免打爆 DB

				if len(list) < batchSize {
					break
				}

			}

		}

		cursor = rules[len(rules)-1].ID

		if len(rules) < batchSize {
			break
		}
	}

}
