package admin

import (
	"oss/adaptor"
	"oss/adaptor/repo/admin"
	"oss/adaptor/repo/admin/gorm"
	"oss/config"
	"oss/utils/logger"

	"go.uber.org/zap"
)

type Service struct {
	conf      *config.Config
	adminUser admin.IUser
	logger    *zap.Logger
}

func NewService(adaptor adaptor.IAdaptor) *Service {
	return &Service{
		conf:      adaptor.GetConfig(),
		adminUser: gorm.NewUserRepo(adaptor),
		logger:    logger.GetLogger().With(zap.String("module", "admin")),
	}
}
