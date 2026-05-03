package accesskey

import (
	"context"
	"oss/adaptor"
	repo "oss/adaptor/repo/accesskey"
	"oss/common"
	"oss/config"
	"oss/consts"
	"oss/service/do"
	"oss/service/dto"
	"oss/utils/tools"
	"strings"
	"time"

	"github.com/samber/lo"
)

type Service struct {
	repo   repo.IAccessKeyRepo
	config *config.Config
}

func NewService(adaptor adaptor.IAdaptor) *Service {
	return &Service{
		repo:   repo.NewAccessKeyRepo(adaptor),
		config: adaptor.GetConfig(),
	}
}

func (srv *Service) CreateAccessKey(ctx context.Context, req *dto.CreateAccessKeyReq) (*dto.CreateAccessKeyResp, common.Errno) {
	if req.UserID <= 0 {
		return nil, common.ParamErr.WithMsg("user_id is required")
	}

	accessKey, err := tools.GenerateRandomKey(24)
	if err != nil {
		return nil, common.ServerErr.WithErr(err)
	}
	secretKey, err := tools.GenerateRandomKey(48)
	if err != nil {
		return nil, common.ServerErr.WithErr(err)
	}

	encryptedSecretKey := tools.Sha256Hash(secretKey)

	// 将毫秒时间戳转换为 time.Time
	var expiresAt *time.Time
	if req.ExpiresAt > 0 {
		t := time.UnixMilli(req.ExpiresAt)
		expiresAt = &t
	}

	ak := &do.CreateAccessKey{
		UserID:     req.UserID,
		AccessKey:  accessKey,
		SecretKey:  encryptedSecretKey,
		Permission: req.Permission,
		ExpiresAt:  expiresAt,
	}

	id, err := srv.repo.CreateAccessKey(ctx, ak)
	if err != nil {
		return nil, common.DatabaseErr.WithErr(err)
	}

	return &dto.CreateAccessKeyResp{
		Id:         id,
		AccessKey:  ak.AccessKey,
		SecretKey:  secretKey, // 返回未加密的明文，仅此一次
		Alias:      req.Alias,
		Status:     consts.AccessKeyStatusEnable,
		Permission: req.Permission,
		ExpiresAt:  req.ExpiresAt,
	}, common.OK
}

func (srv *Service) ListAccessKeys(ctx context.Context, req *dto.ListAccessKeysReq) (*dto.ListAccessKeysResp, common.Errno) {
	if req.UserID <= 0 {
		return nil, common.ParamErr.WithMsg("user_id is required")
	}

	aks, err := srv.repo.ListByFilter(ctx, req.UserID, req.Status)
	if err != nil {
		return nil, common.DatabaseErr.WithErr(err)
	}

	items := lo.Map(aks, func(ak *do.AccessKeyDo, _ int) *dto.AccessKeyItem {
		return &dto.AccessKeyItem{
			ID:         ak.ID,
			AccessKey:  ak.AccessKey,
			Alias:      ak.Alias,
			Status:     ak.Status,
			UserID:     ak.UserID,
			Permission: (ak.Permission),
			ExpiresAt:  ak.ExpiresAt.UnixMilli(),
			LastUsedAt: ak.LastUsedAt.UnixMilli(),
		}
	})

	return &dto.ListAccessKeysResp{Items: items}, common.OK
}

func (srv *Service) GetAccessKey(ctx context.Context, accessKey string) (*dto.AccessKeyItem, common.Errno) {
	if strings.TrimSpace(accessKey) == "" {
		return nil, common.ParamErr.WithMsg("access_key is required")
	}

	ak, err := srv.repo.GetByAccessKey(ctx, accessKey)
	if err != nil {
		return nil, common.ParamErr.WithErr(err)
	}

	return &dto.AccessKeyItem{
		ID:         ak.ID,
		AccessKey:  ak.AccessKey,
		Alias:      ak.Alias,
		Status:     ak.Status,
		UserID:     ak.UserID,
		Permission: ak.Permission,
		ExpiresAt:  ak.ExpiresAt.UnixMilli(),
		LastUsedAt: ak.LastUsedAt.UnixMilli(),
	}, common.OK
}

func (srv *Service) UpdateAccessKeyStatus(ctx context.Context, accessKey string, req *dto.UpdateAccessKeyStatusReq) (*dto.UpdateAccessKeyStatusResp, common.Errno) {
	if strings.TrimSpace(accessKey) == "" {
		return nil, common.ParamErr.WithMsg("access_key is required")
	}
	if req.Status != 0 && req.Status != 1 {
		return nil, common.ParamErr.WithMsg("status must be 0 or 1")
	}

	ak, err := srv.repo.UpdateStatus(ctx, accessKey, req.Status)
	if err != nil {
		return nil, common.DatabaseErr.WithErr(err)
	}

	return &dto.UpdateAccessKeyStatusResp{
		ID:        ak.ID,
		AccessKey: ak.AccessKey,
		Status:    ak.Status,
	}, common.OK
}
