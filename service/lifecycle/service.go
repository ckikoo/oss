package lifecycle

import (
	"strings"
	"time"

	"oss/adaptor"
	bucketRepo "oss/adaptor/repo/bucket"
	lifecycleRepo "oss/adaptor/repo/lifecycle"
	objectRepo "oss/adaptor/repo/object"
	"oss/common"
	"oss/service/do"
	"oss/service/dto"
)

type Service struct {
	repo       lifecycleRepo.ILifecycleRepo
	bucketRepo bucketRepo.IBucketRepo
	objectRepo objectRepo.IObjectRepo
}

func NewService(adaptor adaptor.IAdaptor) *Service {
	return &Service{
		repo:       lifecycleRepo.NewLifecycleRepo(adaptor.GetGORM()),
		bucketRepo: bucketRepo.NewBucketRepo(adaptor.GetGORM()),
		objectRepo: objectRepo.NewObjectRepo(adaptor.GetGORM()),
	}
}

func (srv *Service) CreateLifecycleRule(ctx *common.UserInfoCtx, bucketName string, req *dto.CreateLifecycleRuleReq) (*dto.CreateLifecycleRuleResp, common.Errno) {
	bucket, err := srv.bucketRepo.GetByName(ctx, ctx.UserID, bucketName)
	if err != nil {
		return nil, common.DatabaseErr.WithErr(err)
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
		return nil, common.DatabaseErr.WithErr(err)
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
		return nil, common.DatabaseErr.WithErr(err)
	}

	rules, err := srv.repo.ListLifecycleRules(ctx, bucket.ID)
	if err != nil {
		return nil, common.DatabaseErr.WithErr(err)
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
		return nil, common.DatabaseErr.WithErr(err)
	}

	rule, err := srv.repo.GetLifecycleRule(ctx, bucket.ID, ruleID)
	if err != nil {
		return nil, common.DatabaseErr.WithErr(err)
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
	if strings.TrimSpace(bucketName) == "" {
		return nil, common.ParamErr.WithMsg("bucket_name is required")
	}
	if ruleID <= 0 {
		return nil, common.ParamErr.WithMsg("rule_id is required")
	}

	bucket, err := srv.bucketRepo.GetByName(ctx, ctx.UserID, bucketName)
	if err != nil {
		return nil, common.DatabaseErr.WithErr(err)
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

	rule, err := srv.repo.UpdateLifecycleRule(ctx, bucket.ID, ruleID, update)
	if err != nil {
		return nil, common.DatabaseErr.WithErr(err)
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

func (srv *Service) DeleteLifecycleRule(ctx *common.UserInfoCtx, bucketName string, ruleID int64) common.Errno {
	if strings.TrimSpace(bucketName) == "" {
		return common.ParamErr.WithMsg("bucket_name is required")
	}
	if ruleID <= 0 {
		return common.ParamErr.WithMsg("rule_id is required")
	}

	bucket, err := srv.bucketRepo.GetByName(ctx, ctx.UserID, bucketName)
	if err != nil {
		return common.DatabaseErr.WithErr(err)
	}

	if err := srv.repo.DeleteLifecycleRule(ctx, bucket.ID, ruleID); err != nil {
		return common.DatabaseErr.WithErr(err)
	}

	return common.OK
}
