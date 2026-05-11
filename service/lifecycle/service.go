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

func (srv *Service) UpdateLifecycleRule(ctx *common.UserInfoCtx, bucketName string, ruleID int64, req *dto.UpdateLifecycleRuleReq) (*dto.LifecycleRuleItem, common.Errno) {

	bucket, err := srv.bucketRepo.GetByName(ctx, ctx.UserID, bucketName)
	if err != nil {
		return nil, common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.BucketNotFoundErr)
	}

	if req.RuleName == nil && req.Prefix == nil && req.TransitionDays == nil && req.TransitionStorageClass == nil && req.ExpirationDays == nil && req.Status == nil {
		return nil, common.ParamErr.WithMsg("at least one field must be updated")
	}

	update := &do.UpdateLifecycleRule{
		RuleName:               req.RuleName,
		Prefix:                 req.Prefix,
		TransitionDays:         req.TransitionDays,
		TransitionStorageClass: req.TransitionStorageClass,
		ExpirationDays:         req.ExpirationDays,
		Status:                 req.Status,
	}

	oldRule, err := srv.repo.GetLifecycleRule(ctx, bucket.ID, ruleID)
	if err != nil {
		return nil, common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.ResouceNotFoundErr)
	}

	rule, err := srv.repo.UpdateLifecycleRule(ctx, bucket.ID, ruleID, update)
	if err != nil {
		return nil, common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.ResouceNotFoundErr)
	}

	srv.rds.ClearRuleEvents(ctx, bucket.ID, ruleID, *oldRule.Prefix)
	// go srv.rescheduleExistingObjects(context.Background(), bucket, rule)
	// 2. days 变了，清旧事件
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

func (srv *Service) rescheduleExistingObjects(ctx context.Context, bucket *do.BucketDo, rule *do.LifecycleRuleDo) {
	// 分页扫描，避免一次性 OOM
	var offset int = 0
	const batchSize = 500
	prefix := func() string {
		if rule.Prefix == nil {
			return ""
		}
		return *rule.Prefix
	}()

	for {

		objects, err := srv.objectRepo.ListByBucketWithPrefix(ctx, &do.ListObjectsByBucket{
			BucketID: bucket.ID,
			Prefix:   prefix,
			Limit:    batchSize,
			Offset:   offset,
		})
		if err != nil || len(objects) == 0 {
			break
		}

		for _, obj := range objects {
			now := obj.CreatedAt // 按文件创建时间算，不是当前时间

			if rule.TransitionDays != nil {
				executeTime := now.AddDate(0, 0, int(*rule.TransitionDays))
				if executeTime.After(time.Now()) { // 还没到期才挂，已到期的跳过
					srv.rds.SetLifecycleEvent(ctx,
						bucket.ID, rule.ID, prefix,
						"transition", obj.ObjectKey, executeTime)
				}
			}

			if rule.ExpirationDays != nil {
				executeTime := now.AddDate(0, 0, int(*rule.ExpirationDays))
				if executeTime.After(time.Now()) {
					srv.rds.SetLifecycleEvent(ctx,
						bucket.ID, rule.ID, prefix,
						"expiration", obj.ObjectKey, executeTime)
				}
			}
		}

		offset += batchSize
		time.Sleep(200 * time.Millisecond) // 让出 CPU，避免打爆 DB
	}
}

func (srv *Service) DeleteLifecycleRule(ctx *common.UserInfoCtx, bucketName string, ruleID int64) common.Errno {
	if strings.TrimSpace(bucketName) == "" {
		return common.ParamErr.WithMsg("bucket_name is required")
	}
	if ruleID <= 0 {
		return common.ParamErr.WithMsg("rule_id is required")
	}

	bucket, err := srv.bucketRepo.GetByName(ctx, ctx.UserID, bucketName)
	if err != nil {
		return common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.BucketNotFoundErr)
	}
	if bucket == nil {
		return common.BucketNotFoundErr
	}

	if err := srv.repo.DeleteLifecycleRule(ctx, bucket.ID, ruleID); err != nil {
		return common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.ResouceNotFoundErr)
	}

	return common.OK
}
