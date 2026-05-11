package bucket

import (
	"context"
	"errors"
	"time"

	"oss/adaptor"
	"oss/adaptor/redis"
	bucketRepo "oss/adaptor/repo/bucket"
	gormBucket "oss/adaptor/repo/bucket/gorm"
	lifecycleRepo "oss/adaptor/repo/lifecycle"
	gormLifecycle "oss/adaptor/repo/lifecycle/gorm"
	"oss/adaptor/repo/repoerr"
	"oss/adaptor/tx"
	"oss/common"
	"oss/consts"
	"oss/service/do"
	"oss/service/dto"
	"oss/utils/logger"

	"go.uber.org/zap"
)

type Service struct {
	repo          bucketRepo.IBucketRepo
	lifecycleRepo lifecycleRepo.ILifecycleRepo
	txManager     tx.ITxManager
	logger        *zap.Logger

	lifeRedis redis.ILifecycle
}

func NewService(adaptor adaptor.IAdaptor) *Service {
	return &Service{
		repo:          gormBucket.NewBucketRepo(adaptor),
		lifecycleRepo: gormLifecycle.NewLifecycleRepo(adaptor.GetGORM()),
		txManager:     adaptor.GetTxManager(),
		logger:        logger.GetLogger().With(zap.String("module", "bucket")),
		lifeRedis:     redis.NewLifecycle(adaptor),
	}
}

func (srv *Service) CreateBucket(ctx *common.UserInfoCtx, req *dto.CreateBucketReq) (*dto.CreateBucketResp, common.Errno) {

	region := req.Region
	if region == "" {
		region = "cn-hz"
	}
	storageClass := req.StorageClass
	if storageClass == "" {
		storageClass = consts.StorageClassStandard
	}

	tmp, err := srv.repo.GetByUserAndName(ctx, req.UserID, req.Name)
	if err != nil && !errors.Is(err, repoerr.ErrNotFound) {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	if tmp != nil {
		return nil, common.DatabaseErr.WithMsg("此库已经存在")
	}

	var id int64

	if err = srv.txManager.RunInTx(ctx, func(ctx context.Context, tx tx.Tx) error {
		id, err = srv.repo.CreateBucket(ctx, &do.CreateBucket{
			UserID:       req.UserID,
			Name:         req.Name,
			Region:       region,
			Acl:          req.Acl,
			Versioning:   req.Versioning,
			StorageClass: storageClass,
		})
		if err != nil {
			return err
		}
		ia := consts.StorageClassIA
		archive := consts.StorageClassArchive
		transitionDays30 := int32(30)
		transitionDays90 := int32(90)
		expirationDays180 := int32(180)

		defaultRules := []*do.CreateLifecycleRule{
			{
				BucketID:               id,
				RuleName:               "Default-IA-Transition",
				Status:                 1,
				Prefix:                 nil,
				TransitionDays:         &transitionDays30,
				TransitionStorageClass: &ia,
				ExpirationDays:         nil,
			},
			{
				BucketID:               id,
				RuleName:               "Default-Archive-Transition",
				Status:                 1,
				Prefix:                 nil,
				TransitionDays:         &transitionDays90,
				TransitionStorageClass: &archive,
				ExpirationDays:         nil,
			},
			{
				BucketID:               id,
				RuleName:               "Default-Expiration",
				Status:                 1,
				Prefix:                 nil,
				TransitionDays:         nil,
				TransitionStorageClass: nil,
				ExpirationDays:         &expirationDays180,
			},
		}

		for _, rule := range defaultRules {
			ruleId, err := srv.lifecycleRepo.CreateLifecycleRule(ctx, rule)
			if err != nil {
				return err
			}
			srv.logger.Info("created default lifecycle rule for bucket", zap.Int64("bucket_id", id), zap.Int64("rule_id", ruleId))

		}

		return nil
	}); err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	return &dto.CreateBucketResp{
		ID:           id,
		Name:         req.Name,
		Region:       region,
		Acl:          req.Acl,
		Versioning:   req.Versioning,
		Status:       consts.BucketStatusNormal,
		StorageClass: storageClass,
		ObjectCount:  0,
		StorageSize:  0,
		CreatedAt:    time.Now().UnixMilli(),
		UpdatedAt:    time.Now().UnixMilli(),
	}, common.OK
}

func (srv *Service) ListBuckets(ctx *common.UserInfoCtx, req *dto.ListBucketsReq) (*dto.ListBucketsResp, common.Errno) {
	buckets, err := srv.repo.ListByFilter(ctx, ctx.UserID, req.Status)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}
	items := make([]*dto.BucketItem, 0, len(buckets))
	for _, bucketDo := range buckets {
		items = append(items, &dto.BucketItem{
			ID:           bucketDo.ID,
			UserID:       bucketDo.UserID,
			Name:         bucketDo.Name,
			Region:       bucketDo.Region,
			Acl:          bucketDo.Acl,
			Versioning:   bucketDo.Versioning,
			Status:       bucketDo.Status,
			StorageClass: bucketDo.StorageClass,
			ObjectCount:  bucketDo.ObjectCount,
			StorageSize:  bucketDo.StorageSize,
			CreatedAt:    bucketDo.CreatedAt.UnixMilli(),
			UpdatedAt:    bucketDo.UpdatedAt.UnixMilli(),
		})
	}
	return &dto.ListBucketsResp{Items: items}, common.OK
}

func (srv *Service) GetBucket(ctx *common.UserInfoCtx, name string) (*dto.BucketItem, common.Errno) {
	bucketDo, err := srv.repo.GetByUserAndName(ctx, ctx.UserID, name)
	if err != nil {
		return nil, common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.BucketNotFoundErr)
	}
	if bucketDo == nil {
		return nil, common.BucketNotFoundErr
	}
	return &dto.BucketItem{
		ID:           bucketDo.ID,
		UserID:       bucketDo.UserID,
		Name:         bucketDo.Name,
		Region:       bucketDo.Region,
		Acl:          bucketDo.Acl,
		Versioning:   bucketDo.Versioning,
		Status:       bucketDo.Status,
		StorageClass: bucketDo.StorageClass,
		ObjectCount:  bucketDo.ObjectCount,
		StorageSize:  bucketDo.StorageSize,
		CreatedAt:    bucketDo.CreatedAt.UnixMilli(),
		UpdatedAt:    bucketDo.UpdatedAt.UnixMilli(),
	}, common.OK
}

func (srv *Service) UpdateBucket(ctx *common.UserInfoCtx, name string, req *dto.UpdateBucketReq) (*dto.UpdateBucketResp, common.Errno) {
	if req.Acl == nil && req.Versioning == nil && req.Status == nil && req.StorageClass == "" {
		return nil, common.ParamErr.WithMsg("no update fields")
	}

	// First check if bucket exists and belongs to user
	info, err := srv.repo.GetByUserAndName(ctx, ctx.UserID, name)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	if info == nil {
		return nil, common.BucketNotFoundErr
	}

	bucketDo, err := srv.repo.UpdateBucket(ctx, ctx.UserID, info.ID, name, &do.UpdateBucket{
		Acl:          req.Acl,
		Versioning:   req.Versioning,
		Status:       req.Status,
		StorageClass: req.StorageClass,
	})

	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	return &dto.UpdateBucketResp{
		ID:           bucketDo.ID,
		UserID:       bucketDo.UserID,
		Name:         bucketDo.Name,
		Region:       bucketDo.Region,
		Acl:          bucketDo.Acl,
		Versioning:   bucketDo.Versioning,
		Status:       bucketDo.Status,
		StorageClass: bucketDo.StorageClass,
		ObjectCount:  bucketDo.ObjectCount,
		StorageSize:  bucketDo.StorageSize,
		CreatedAt:    bucketDo.CreatedAt.UnixMilli(),
		UpdatedAt:    bucketDo.UpdatedAt.UnixMilli(),
	}, common.OK
}

func (srv *Service) DeleteBucket(ctx *common.UserInfoCtx, bucketID int64, buckName string) common.Errno {

	list, err := srv.lifecycleRepo.ListLifecycleRules(ctx, bucketID)
	if err != nil {
		srv.logger.Error("failed to list lifecycle rules for bucket", zap.Int64("bucket_id", bucketID), zap.Error(err))
		return common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	if err = srv.txManager.RunInTx(ctx, func(ctx1 context.Context, tx tx.Tx) error {
		for _, rule := range list {
			if err = srv.lifecycleRepo.DeleteLifecycleRule(ctx1, bucketID, rule.ID); err != nil {
				return err
			}
		}

		if err := srv.repo.DeleteBucket(ctx1, ctx.UserID, bucketID, buckName); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	for _, rule := range list {
		// redis 相关的 lifecycle 数据也删除掉
		if err = srv.lifeRedis.ClearRuleEvents(ctx, bucketID, rule.ID, *rule.Prefix); err != nil {
			srv.logger.Warn("failed to delete lifecycle rules from redis", zap.Int64("bucket_id", bucketID), zap.Error(err))
		}
	}

	return common.OK
}
