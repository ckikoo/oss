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

func handlerScanTableLifecycleEvents(ctx context.Context, a adaptor.IAdaptor) {
	repo := gormLifecycle.NewLifecycleRepo(a.GetGORM())
	objRepo := gormObject.NewObjectRepo(a)
	rds := redis.NewLifecycle(a)

	const batchSize = 100
	var cursor int64 = 0

	// ── 分批扫 lifecycle rules ───────────────────────────────
	for {
		rules, err := repo.ListAllActiveLifecycleRulesByCursor(ctx, cursor, batchSize)
		if err != nil {
			log.Error("handlerScanTableLifecycleEvents: list rules", zap.Error(err))
			return
		}

		if len(rules) == 0 {
			break
		}

		for _, rule := range rules {
			prefix := ""
			if rule.Prefix != nil {
				prefix = *rule.Prefix
			}

			// ── 分批扫该规则下的 objects ─────────────────────
			offset := 0 // ← 移到内层循环外
			for {
				list, err := objRepo.ListByBucketWithPrefix(ctx, &do.ListObjectsByBucket{
					BucketID: rule.BucketID,
					Prefix:   prefix,
					Limit:    batchSize,
					Offset:   offset,
				})
				if err != nil {
					log.Error("handlerScanTableLifecycleEvents: list objects",
						zap.Int64("bucketID", rule.BucketID),
						zap.Int64("ruleID", rule.ID),
						zap.Error(err))
					break // 这条 rule 跳过，继续下一条
				}

				for _, obj := range list {
					// transition
					if rule.TransitionDays != nil && *rule.TransitionDays > 0 {
						executeTime := obj.CreatedAt.AddDate(0, 0, int(*rule.TransitionDays))
						if err := rds.SetLifecycleEvent(ctx, rule.BucketID, rule.ID, prefix, "transition", obj.ObjectKey, executeTime); err != nil {
							log.Warn("handlerScanTableLifecycleEvents: set transition event",
								zap.String("objectKey", obj.ObjectKey), zap.Error(err))
						}
					}
					// expiration
					if rule.ExpirationDays != nil && *rule.ExpirationDays > 0 {
						executeTime := obj.CreatedAt.AddDate(0, 0, int(*rule.ExpirationDays))
						if err := rds.SetLifecycleEvent(ctx, rule.BucketID, rule.ID, prefix, "expiration", obj.ObjectKey, executeTime); err != nil {
							log.Warn("handlerScanTableLifecycleEvents: set expiration event",
								zap.String("objectKey", obj.ObjectKey), zap.Error(err))
						}
					}
				}

				offset += len(list)
				time.Sleep(50 * time.Millisecond) // 200ms 太保守，50ms 够了

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
