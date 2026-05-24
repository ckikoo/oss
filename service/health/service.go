package health

import (
	"context"
	"errors"
	"time"

	"oss/adaptor"
	"oss/common"
	"oss/service/dto"
)

// StatusOK 表示检查通过
const StatusOK = "ok"

// StatusError 表示检查失败
const StatusError = "error"

// IHealthService 定义健康检查接口
type IHealthService interface {
	// Liveness 检查服务是否存活（轻量级）
	Liveness(ctx context.Context) (*dto.HealthResp, common.Errno)
	// Readiness 检查服务是否准备就绪（包含依赖检查）
	Readiness(ctx context.Context) (*dto.HealthResp, common.Errno)
}

// Service 健康检查服务实现
type Service struct {
	checkTimeout time.Duration
	adaptor      adaptor.IAdaptor
	dbPing       func(context.Context) error
	redisPing    func(context.Context) error
	storageCheck func(context.Context) error
}

// NewService 创建新的健康检查服务实例
func NewService(adp adaptor.IAdaptor) IHealthService {
	return &Service{
		checkTimeout: 5 * time.Second,
		adaptor:      adp,
		dbPing: func(ctx context.Context) error {
			if adp.GetDB() == nil {
				return errors.New("database not initialized")
			}
			return adp.GetDB().PingContext(ctx)
		},
		redisPing: func(ctx context.Context) error {
			if adp.GetRedis() == nil {
				return errors.New("redis not initialized")
			}
			return adp.GetRedis().Ping(ctx).Err()
		},
		storageCheck: func(ctx context.Context) error {
			if adp.GetStorage() == nil {
				return errors.New("storage not initialized")
			}
			// 存储层没有 context 参数的 ping，这里简单验证初始化状态
			return nil
		},
	}
}

// Liveness 实现轻量级存活性检查，仅检查服务进程是否存活
func (s *Service) Liveness(ctx context.Context) (*dto.HealthResp, common.Errno) {
	return &dto.HealthResp{
		Status:    StatusOK,
		Timestamp: time.Now().UTC(),
		Checks:    make(map[string]dto.HealthCheckItem),
	}, common.OK
}

// Readiness 检查服务是否准备就绪，包括数据库、缓存和存储的连接检查
func (s *Service) Readiness(ctx context.Context) (*dto.HealthResp, common.Errno) {
	checkCtx, cancel := context.WithTimeout(ctx, s.checkTimeout)
	defer cancel()

	checks := map[string]dto.HealthCheckItem{
		"mysql":   s.runCheck(checkCtx, s.dbPing),
		"redis":   s.runCheck(checkCtx, s.redisPing),
		"storage": s.runCheck(checkCtx, s.storageCheck),
	}

	return &dto.HealthResp{
		Status:    overallStatus(checks),
		Timestamp: time.Now().UTC(),
		Checks:    checks,
	}, common.OK
}

// runCheck 执行单个检查项
func (s *Service) runCheck(ctx context.Context, check func(context.Context) error) dto.HealthCheckItem {
	start := time.Now()
	if err := check(ctx); err != nil {
		return dto.HealthCheckItem{
			Status:    StatusError,
			LatencyMS: elapsedMS(start),
			Error:     err.Error(),
		}
	}
	return dto.HealthCheckItem{
		Status:    StatusOK,
		LatencyMS: elapsedMS(start),
	}
}

// overallStatus 根据所有检查项判断总体状态
func overallStatus(checks map[string]dto.HealthCheckItem) string {
	for _, item := range checks {
		if item.Status != StatusOK {
			return StatusError
		}
	}
	return StatusOK
}

// elapsedMS 计算耗时（毫秒）
func elapsedMS(start time.Time) int64 {
	return time.Since(start).Milliseconds()
}
