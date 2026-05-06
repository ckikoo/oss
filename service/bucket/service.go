package bucket

import (
	"time"

	"oss/adaptor"
	bucketRepo "oss/adaptor/repo/bucket"
	lifecycleRepo "oss/adaptor/repo/lifecycle"
	"oss/common"
	"oss/consts"
	"oss/service/do"
	"oss/service/dto"

	"gorm.io/gorm"
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

func (srv *Service) CreateBucket(ctx *common.UserInfoCtx, req *dto.CreateBucketReq) (*dto.CreateBucketResp, common.Errno) {
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

	tmp, err := srv.repo.GetByUserAndName(ctx, req.UserID, req.Name)
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, common.DatabaseErr.WithErr(err)
	}

	if tmp != nil {
		return nil, common.DatabaseErr.WithMsg("此库已经存在")
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

func (srv *Service) GetBucket(ctx *common.UserInfoCtx, name string) (*dto.BucketItem, common.Errno) {
	if name == "" {
		return nil, common.ParamErr.WithMsg("bucket name is required")
	}
	bucketDo, err := srv.repo.GetByUserAndName(ctx, ctx.UserID, name)
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

func (srv *Service) UpdateBucket(ctx *common.UserInfoCtx, name string, req *dto.UpdateBucketReq) (*dto.UpdateBucketResp, common.Errno) {
	if name == "" {
		return nil, common.ParamErr.WithMsg("bucket name is required")
	}
	if req.Acl == nil && req.Versioning == nil && req.Status == nil && req.StorageClass == "" {
		return nil, common.ParamErr.WithMsg("no update fields")
	}

	// First check if bucket exists and belongs to user
	_, err := srv.repo.GetByUserAndName(ctx, ctx.UserID, name)
	if err != nil {
		return nil, common.ParamErr.WithErr(err)
	}

	bucketDo, err := srv.repo.UpdateBucket(ctx, ctx.UserID, name, &do.UpdateBucket{
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

func (srv *Service) DeleteBucket(ctx *common.UserInfoCtx, name string) common.Errno {
	if name == "" {
		return common.ParamErr.WithMsg("bucket name is required")
	}
	// First check if bucket exists and belongs to user
	_, err := srv.repo.GetByUserAndName(ctx, ctx.UserID, name)
	if err != nil {
		return common.ParamErr.WithErr(err)
	}
	if err := srv.repo.DeleteBucket(ctx, ctx.UserID, name); err != nil {
		return common.DatabaseErr.WithErr(err)
	}
	return common.OK
}
