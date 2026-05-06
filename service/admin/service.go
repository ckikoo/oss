package admin

import (
	"oss/adaptor"
	"oss/adaptor/repo/admin"
	"oss/config"
)

type Service struct {
	conf      *config.Config
	adminUser *admin.User
}

func NewService(adaptor adaptor.IAdaptor) *Service {
	return &Service{
		conf:      adaptor.GetConfig(),
		adminUser: admin.NewUserRepo(adaptor),
	}
}
