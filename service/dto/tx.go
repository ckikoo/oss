package dto

import "time"

// TransactionLogInfo 事务日志信息响应
type TransactionLogInfo struct {
	ID            int64     `json:"id"`
	TransactionID string    `json:"transaction_id"`
	Operation     string    `json:"operation"`
	Status        int32     `json:"status"`
	UserID        *int64    `json:"user_id,omitempty"`
	BucketID      *int64    `json:"bucket_id,omitempty"`
	ObjectKey     *string   `json:"object_key,omitempty"`
	ErrorMsg      *string   `json:"error_msg,omitempty"`
	DurationMs    int32     `json:"duration_ms"`
	CreatedAt     time.Time `json:"created_at"`
}

// ListTransactionLogsResp 列出事务日志响应
type ListTransactionLogsResp struct {
	Logs       []*TransactionLogInfo `json:"logs"`
	TotalCount int64                 `json:"total_count"`
}

// CreateTransactionLogReq 创建事务日志请求
type CreateTransactionLogReq struct {
	TransactionID string  `json:"transaction_id" binding:"required"`
	Operation     string  `json:"operation" binding:"required,oneof=START COMMIT ROLLBACK"`
	Status        int32   `json:"status" binding:"required,min=0,max=2"`
	UserID        *int64  `json:"user_id,omitempty"`
	BucketID      *int64  `json:"bucket_id,omitempty"`
	ObjectKey     *string `json:"object_key,omitempty"`
	ErrorMsg      *string `json:"error_msg,omitempty"`
	DurationMs    int32   `json:"duration_ms"`
}
