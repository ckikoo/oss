package lifecycle

import (
	"context"
	"strings"
	"time"

	"oss/adaptor"
	"oss/adaptor/redis"
	bucketRepo "oss/adaptor/repo/bucket"
	gormBucket "oss/adaptor/repo/bucket/gorm"
	lifecycleRepo "oss/adaptor/repo/lifecycle"
	"oss/adaptor/repo/lifecycle/gorm"
	objectRepo "oss/adaptor/repo/object"
	gormObject "oss/adaptor/repo/object/gorm"
	"oss/common"
	"oss/service/do"
	"oss/service/dto"
	"oss/utils/logger"

	"go.uber.org/zap"
)

type Service struct {
	repo       lifecycleRepo.ILifecycleRepo
	rds        redis.ILifecycle
	bucketRepo bucketRepo.IBucketRepo
	objectRepo objectRepo.IObjectRepo
	logger     *zap.Logger
}

func NewService(adaptor adaptor.IAdaptor) *Service {
	return &Service{
		repo:       gorm.NewLifecycleRepo(adaptor.GetGORM()),
		bucketRepo: gormBucket.NewBucketRepo(adaptor),
		objectRepo: gormObject.NewObjectRepo(adaptor),
		rds:        redis.NewLifecycle(adaptor),
		logger:     logger.GetLogger().With(zap.String("module", "lifecycle")),
	}
}

func (srv *Service) CreateLifecycleRule(ctx *common.UserInfoCtx, bucketName string, req *dto.CreateLifecycleRuleReq) (*dto.CreateLifecycleRuleResp, common.Errno) {
	bucket, err := srv.bucketRepo.GetByName(ctx, ctx.UserID, bucketName)
	if err != nil {
		return nil, common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.BucketNotFoundErr)
	}
	if bucket == nil {
		return nil, common.BucketNotFoundErr
	}

	status := int32(1)
	if req.Status != nil {
		status = *req.Status
	}

	create := &do.CreateLifecycleRule{
		BucketID:                        bucket.ID,
		RuleName:                        req.RuleName,
		Status:                          status,
		Prefix:                          req.Prefix,
		TransitionDays:                  req.TransitionDays,
		TransitionStorageClass:          req.TransitionStorageClass,
		ExpirationDays:                  req.ExpirationDays,
		NoncurrentVersionExpirationDays: nil,
		AbortIncompleteMultipartDays:    nil,
	}

	ruleID, err := srv.repo.CreateLifecycleRule(ctx, create)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	now := time.Now().UnixMilli()
	return &dto.CreateLifecycleRuleResp{
		RuleID:                 ruleID,
		RuleName:               req.RuleName,
		Prefix:                 req.Prefix,
		TransitionDays:         req.TransitionDays,
		TransitionStorageClass: req.TransitionStorageClass,
		ExpirationDays:         req.ExpirationDays,
		Status:                 status,
		CreatedAt:              now,
		UpdatedAt:              now,
	}, common.OK
}

func (srv *Service) ListLifecycleRules(ctx *common.UserInfoCtx, bucketName string) (*dto.ListLifecycleRulesResp, common.Errno) {
	if strings.TrimSpace(bucketName) == "" {
		return nil, common.ParamErr.WithMsg("bucket_name is required")
	}

	bucket, err := srv.bucketRepo.GetByName(ctx, ctx.UserID, bucketName)
	if err != nil {
		return nil, common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.BucketNotFoundErr)
	}
	if bucket == nil {
		return nil, common.BucketNotFoundErr
	}

	rules, err := srv.repo.ListLifecycleRules(ctx, bucket.ID)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	items := make([]*dto.LifecycleRuleItem, 0, len(rules))
	for _, rule := range rules {
		items = append(items, &dto.LifecycleRuleItem{
			RuleID:                 rule.ID,
			RuleName:               rule.RuleName,
			Prefix:                 rule.Prefix,
			TransitionDays:         rule.TransitionDays,
			TransitionStorageClass: rule.TransitionStorageClass,
			ExpirationDays:         rule.ExpirationDays,
			Status:                 rule.Status,
			CreatedAt:              rule.CreatedAt.UnixMilli(),
			UpdatedAt:              rule.UpdatedAt.UnixMilli(),
		})
	}

	return &dto.ListLifecycleRulesResp{Items: items}, common.OK
}

func (srv *Service) GetLifecycleRule(ctx *common.UserInfoCtx, bucketName string, ruleID int64) (*dto.LifecycleRuleItem, common.Errno) {
	if strings.TrimSpace(bucketName) == "" {
		return nil, common.ParamErr.WithMsg("bucket_name is required")
	}
	if ruleID <= 0 {
		return nil, common.ParamErr.WithMsg("rule_id is required")
	}

	bucket, err := srv.bucketRepo.GetByName(ctx, ctx.UserID, bucketName)
	if err != nil {
		return nil, common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.BucketNotFoundErr)
	}

	rule, err := srv.repo.GetLifecycleRule(ctx, bucket.ID, ruleID)
	if err != nil {
		return nil, common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.ResouceNotFoundErr)
	}

	return &dto.LifecycleRuleItem{
		RuleID:                 rule.ID,
		RuleName:               rule.RuleName,
		Prefix:                 rule.Prefix,
		TransitionDays:         rule.TransitionDays,
		TransitionStorageClass: rule.TransitionStorageClass,
		ExpirationDays:         rule.ExpirationDays,
		Status:                 rule.Status,
		CreatedAt:              rule.CreatedAt.UnixMilli(),
		UpdatedAt:              rule.UpdatedAt.UnixMilli(),
	}, common.OK
}

type ruleChanges struct {
	NeedClearRedis bool
	NeedFullRescan bool
	OldPrefix      string // 清 Redis 时用旧 prefix
}

func detectRuleChanges(old *do.LifecycleRuleDo, req *dto.UpdateLifecycleRuleReq) ruleChanges {
	oldPrefix := ""
	if old.Prefix != nil {
		oldPrefix = *old.Prefix
	}

	daysChanged := transitionDaysChanged(old, req) || expirationDaysChanged(old, req)
	prefixChanged := req.Prefix != nil && !prefixEqual(old.Prefix, req.Prefix)
	statusDisabled := req.Status != nil && *req.Status == 0 && old.Status == 1
	statusEnabled := req.Status != nil && *req.Status == 1 && old.Status == 0
	storageClassChanged := req.TransitionStorageClass != nil &&
		!storageClassEqual(old.TransitionStorageClass, req.TransitionStorageClass)

	return ruleChanges{
		NeedClearRedis: daysChanged || prefixChanged || statusDisabled || statusEnabled,
		NeedFullRescan: daysChanged || prefixChanged || statusEnabled || storageClassChanged,
		OldPrefix:      oldPrefix,
	}
}

func prefixEqual(a *string, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func storageClassEqual(a *string, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func transitionDaysChanged(old *do.LifecycleRuleDo, req *dto.UpdateLifecycleRuleReq) bool {
	if req.TransitionDays == nil {
		return false // 没改
	}
	if old.TransitionDays == nil {
		return true // 从无到有
	}
	return *old.TransitionDays != *req.TransitionDays
}

func expirationDaysChanged(old *do.LifecycleRuleDo, req *dto.UpdateLifecycleRuleReq) bool {
	if req.ExpirationDays == nil {
		return false
	}
	if old.ExpirationDays == nil {
		return true
	}
	return *old.ExpirationDays != *req.ExpirationDays
}
func (srv *Service) UpdateLifecycleRule(ctx *common.UserInfoCtx, bucketName string, ruleID int64, req *dto.UpdateLifecycleRuleReq) (*dto.LifecycleRuleItem, common.Errno) {

	bucket, err := srv.bucketRepo.GetByName(ctx, ctx.UserID, bucketName)
	if err != nil {
		return nil, common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.BucketNotFoundErr)
	}

	if req.RuleName == nil && req.Prefix == nil &&
		req.TransitionDays == nil && req.TransitionStorageClass == nil &&
		req.ExpirationDays == nil && req.Status == nil {
		return nil, common.ParamErr.WithMsg("at least one field must be updated")
	}

	oldRule, err := srv.repo.GetLifecycleRule(ctx, bucket.ID, ruleID)
	if err != nil {
		return nil, common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.ResouceNotFoundErr)
	}

	changes := detectRuleChanges(oldRule, req)

	update := &do.UpdateLifecycleRule{
		RuleName:               req.RuleName,
		Prefix:                 req.Prefix,
		TransitionDays:         req.TransitionDays,
		TransitionStorageClass: req.TransitionStorageClass,
		ExpirationDays:         req.ExpirationDays,
		Status:                 req.Status,
	}

	rule, err := srv.repo.UpdateLifecycleRule(ctx, bucket.ID, ruleID, update)
	if err != nil {
		return nil, common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.ResouceNotFoundErr)
	}

	// 清 Redis（用旧 prefix，因为旧事件是按旧 prefix 存的）
	if changes.NeedClearRedis {
		if err := srv.rds.ClearRuleEvents(ctx, bucket.ID, ruleID, changes.OldPrefix); err != nil {
			// 非致命，记录日志，下次扫描会兜底
			srv.logger.Warn("failed to clear lifecycle redis events",
				zap.Int64("bucketID", bucket.ID),
				zap.Int64("ruleID", ruleID),
				zap.Error(err))
		}
	}

	if changes.NeedFullRescan {
		go srv.rescheduleExistingObjects(context.Background(), bucket, rule)
	}

	// 2. days 变了，清旧事件
	return toLifecycleRuleItem(rule), common.OK
}
func toLifecycleRuleItem(rule *do.LifecycleRuleDo) *dto.LifecycleRuleItem {
	return &dto.LifecycleRuleItem{
		RuleID:                 rule.ID,
		RuleName:               rule.RuleName,
		Prefix:                 rule.Prefix,
		TransitionDays:         rule.TransitionDays,
		TransitionStorageClass: rule.TransitionStorageClass,
		ExpirationDays:         rule.ExpirationDays,
		Status:                 rule.Status,
		CreatedAt:              rule.CreatedAt.UnixMilli(),
		UpdatedAt:              rule.UpdatedAt.UnixMilli(),
	}
}

func (srv *Service) rescheduleExistingObjects(
	ctx context.Context,
	bucket *do.BucketDo,
	rule *do.LifecycleRuleDo,
) {
	newPrefix := ""
	if rule.Prefix != nil {
		newPrefix = *rule.Prefix
	}

	srv.logger.Info("start reschedule existing objects",
		zap.Int64("bucketID", bucket.ID),
		zap.Int64("ruleID", rule.ID),
		zap.String("prefix", newPrefix))

	var cursor int64 = 0
	const batchSize = 200
	total := 0

	for {
		select {
		case <-ctx.Done():
			srv.logger.Warn("rescheduleExistingObjects cancelled",
				zap.Int64("ruleID", rule.ID),
				zap.Int("processed", total))
			return
		default:
		}

		list, err := srv.objectRepo.ListByBucketWithPrefix(ctx, &do.ListObjectsByBucket{
			BucketID: rule.BucketID,
			Prefix:   newPrefix,
			Limit:    batchSize,
			Cursor:   cursor,
		})
		if err != nil {
			srv.logger.Error("rescheduleExistingObjects list objects failed",
				zap.Int64("ruleID", rule.ID),
				zap.Int64("cursor", cursor),
				zap.Error(err))
			return
		}
		if len(list) == 0 {
			break
		}

		now := time.Now()
		for _, obj := range list {
			srv.scheduleObjectEvents(ctx, bucket.ID, rule, newPrefix, obj, now)
		}

		total += len(list)
		cursor = list[len(list)-1].ID

		time.Sleep(50 * time.Millisecond)

		if len(list) < batchSize {
			break
		}
	}

	srv.logger.Info("rescheduleExistingObjects done",
		zap.Int64("ruleID", rule.ID),
		zap.Int("total", total))
}

// ── scheduleObjectEvents ────────────────────────────────────────────
// PutObject 和 reschedule 共用同一个函数，逻辑收拢
func (srv *Service) scheduleObjectEvents(
	ctx context.Context,
	bucketID int64,
	rule *do.LifecycleRuleDo,
	prefix string,
	obj *do.ObjectDo,
	now time.Time,
) {
	// rule 被禁用，不入队
	if rule.Status == 0 {
		return
	}

	// transition
	if rule.TransitionDays != nil && *rule.TransitionDays > 0 {
		executeTime := obj.CreatedAt.AddDate(0, 0, int(*rule.TransitionDays))
		if executeTime.After(now) { // 已过期的不入队，直接跳过
			if err := srv.rds.SetLifecycleEvent(ctx, bucketID, rule.ID, prefix,
				"transition", obj.ObjectKey, executeTime); err != nil {
				srv.logger.Warn("scheduleObjectEvents set transition failed",
					zap.String("objectKey", obj.ObjectKey), zap.Error(err))
			}
		}
	}

	// expiration
	if rule.ExpirationDays != nil && *rule.ExpirationDays > 0 {
		executeTime := obj.CreatedAt.AddDate(0, 0, int(*rule.ExpirationDays))
		if executeTime.After(now) {
			if err := srv.rds.SetLifecycleEvent(ctx, bucketID, rule.ID, prefix,
				"expiration", obj.ObjectKey, executeTime); err != nil {
				srv.logger.Warn("scheduleObjectEvents set expiration failed",
					zap.String("objectKey", obj.ObjectKey), zap.Error(err))
			}
		}
	}
}
func (srv *Service) DeleteLifecycleRule(ctx *common.UserInfoCtx, bucketName string, ruleID int64) common.Errno {

	bucket, err := srv.bucketRepo.GetByName(ctx, ctx.UserID, bucketName)
	if err != nil {
		return common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.BucketNotFoundErr)
	}
	if bucket == nil {
		return common.BucketNotFoundErr
	}

	oldRule, err := srv.repo.GetLifecycleRule(ctx, bucket.ID, ruleID)
	if err != nil {
		return common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.ResouceNotFoundErr)
	}

	if err := srv.repo.DeleteLifecycleRule(ctx, bucket.ID, ruleID); err != nil {
		return common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.ResouceNotFoundErr)
	}

	prefix := ""
	if oldRule.Prefix != nil {
		prefix = *oldRule.Prefix
	}

	srv.rds.ClearRuleEvents(ctx, bucket.ID, ruleID, prefix)

	return common.OK
}
