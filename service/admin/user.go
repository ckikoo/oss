package admin

import (
	"context"
	"oss/common"
	"oss/service/do"
	"oss/service/dto"
)

func (srv *Service) CreateUser(ctx context.Context, req *dto.CreateUserReq) (int64, common.Errno) {
	id, err := srv.adminUser.CreateUser(ctx, &do.CreateUser{
		Email:        req.Email,
		StorageQuota: req.StorageQuota,
	})

	if err != nil {
		return 0, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	return id, common.OK
}

func (srv *Service) GetUserByID(ctx context.Context, id int64) (*dto.User, error) {
	user, err := srv.adminUser.GetUserInfoById(ctx, id)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	return &dto.User{
		ID:           user.ID,
		Email:        user.Email,
		Status:       user.Status,
		StorageQuota: user.StorageQuota,
		StorageUsed:  user.StorageUsed,
		CreatedAt:    user.CreatedAt,
		UpdatedAt:    user.UpdatedAt,
	}, nil
}
