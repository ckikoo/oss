package admin

import (
	"oss/adaptor"
	"oss/adaptor/repo/admin"
	"oss/adaptor/repo/admin/gorm"
	"oss/config"
)

type Service struct {
	conf      *config.Config
	adminUser admin.IUser
}

func NewService(adaptor adaptor.IAdaptor) *Service {
	return &Service{
		conf:      adaptor.GetConfig(),
		adminUser: gorm.NewUserRepo(adaptor.GetGORM()),
	}
}
