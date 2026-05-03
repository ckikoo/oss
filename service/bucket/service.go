package bucket

import (
	"context"
	"time"

	"oss/adaptor"
	bucketRepo "oss/adaptor/repo/bucket"
	lifecycleRepo "oss/adaptor/repo/lifecycle"
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
}

func NewService(adaptor adaptor.IAdaptor) *Service {
	return &Service{
		repo:          bucketRepo.NewBucketRepo(adaptor),
		lifecycleRepo: lifecycleRepo.NewLifecycleRepo(adaptor),
	}
}

func (srv *Service) CreateBucket(ctx context.Context, req *dto.CreateBucketReq) (*dto.CreateBucketResp, common.Errno) {
	if req.UserID <= 0 {
		return nil, common.ParamErr.WithMsg("user_id is required")
	}
	if req.Name == "" {
		return nil, common.ParamErr.WithMsg("name is required")
	}
	region := req.Region
	if region == "" {
		region = "cn-hz"
	}
	storageClass := req.StorageClass
	if storageClass == "" {
		storageClass = consts.StorageClassStandard
	}

	id, err := srv.repo.CreateBucket(ctx, &do.CreateBucket{
		UserID:       req.UserID,
		Name:         req.Name,
		Region:       region,
		Acl:          req.Acl,
		Versioning:   req.Versioning,
		StorageClass: storageClass,
	})
	if err != nil {
		return nil, common.DatabaseErr.WithErr(err)
	}

	// 创建默认的生命周期规则
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
		if _, err := srv.lifecycleRepo.CreateLifecycleRule(ctx, rule); err != nil {
			// Log error but don't fail bucket creation if default rules fail
			logger.Warn("failed to create default lifecycle rule", zap.String("rule_name", rule.RuleName), zap.Error(err))
		}
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

func (srv *Service) ListBuckets(ctx context.Context, req *dto.ListBucketsReq) (*dto.ListBucketsResp, common.Errno) {
	if req.UserID <= 0 {
		return nil, common.ParamErr.WithMsg("user_id is required")
	}
	buckets, err := srv.repo.ListByFilter(ctx, req.UserID, req.Status)
	if err != nil {
		return nil, common.DatabaseErr.WithErr(err)
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

func (srv *Service) GetBucket(ctx context.Context, name string) (*dto.BucketItem, common.Errno) {
	if name == "" {
		return nil, common.ParamErr.WithMsg("bucket name is required")
	}
	bucketDo, err := srv.repo.GetByName(ctx, name)
	if err != nil {
		return nil, common.ParamErr.WithErr(err)
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

func (srv *Service) UpdateBucket(ctx context.Context, name string, req *dto.UpdateBucketReq) (*dto.UpdateBucketResp, common.Errno) {
	if name == "" {
		return nil, common.ParamErr.WithMsg("bucket name is required")
	}
	if req.Region == "" && req.Acl == nil && req.Versioning == nil && req.Status == nil && req.StorageClass == "" {
		return nil, common.ParamErr.WithMsg("no update fields")
	}

	bucketDo, err := srv.repo.UpdateBucket(ctx, name, &do.UpdateBucket{
		Region:       req.Region,
		Acl:          req.Acl,
		Versioning:   req.Versioning,
		Status:       req.Status,
		StorageClass: req.StorageClass,
	})
	if err != nil {
		return nil, common.DatabaseErr.WithErr(err)
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

func (srv *Service) DeleteBucket(ctx context.Context, name string) common.Errno {
	if name == "" {
		return common.ParamErr.WithMsg("bucket name is required")
	}
	if err := srv.repo.DeleteBucket(ctx, name); err != nil {
		return common.DatabaseErr.WithErr(err)
	}
	return common.OK
}
