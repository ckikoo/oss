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

		// 这边的 ListAllActiveLifecycleRulesByCursor 内部已经按 ID 升序了，所以直接用最后一个 ID 作为下一轮的 cursor 就行

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
			var objcurrsor int64 = 0 // ← 移到内层循环外
			for {

				// 优化，可以时间区域间分批，先按时间范围扫出 rule ID 列表，再根据 ID 列表扫对象，这样可以减少不必要的对象扫描
				// 过去三天，进行重新对齐,
				list, err := objRepo.ListByBucketWithPrefix(ctx, &do.ListObjectsByBucket{
					BucketID: rule.BucketID,
					Prefix:   prefix,
					Limit:    batchSize,
					Cursor:   objcurrsor,
				})
				if err != nil {
					log.Error("handlerScanTableLifecycleEvents: list objects",
						zap.Int64("bucketID", rule.BucketID),
						zap.Int64("ruleID", rule.ID),
						zap.Error(err))
					break // 这条 rule 跳过，继续下一条
				}

				if len(list) == 0 {
					break
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

				if len(list) < batchSize {
					break
				}

				objcurrsor = list[len(list)-1].ID
				time.Sleep(50 * time.Millisecond)
			}
		}

		cursor = rules[len(rules)-1].ID
		if len(rules) < batchSize {
			break
		}
	}
}
