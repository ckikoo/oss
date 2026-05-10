package auth

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
)

// ITxHandler 事务日志处理器接口
type ITxHandler interface {
	// CreateTransactionLog 创建事务日志
	CreateTransactionLog(ctx context.Context, c *app.RequestContext)

	// ListTransactionLogs 列出事务日志
	ListTransactionLogs(ctx context.Context, c *app.RequestContext)

	// GetTransactionLogsByID 根据事务ID获取日志
	GetTransactionLogsByID(ctx context.Context, c *app.RequestContext)

	// CleanupExpiredLogs 清理过期日志
	CleanupExpiredLogs(ctx context.Context, c *app.RequestContext)
}
