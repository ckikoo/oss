package do

import "time"

// TransactionLogDo 事务日志数据对象
type TransactionLogDo struct {
	ID            int64
	TransactionID string
	Operation     string // START/COMMIT/ROLLBACK
	Status        int32  // 0=进行中 1=成功 2=失败
	UserID        *int64
	BucketID      *int64
	ObjectKey     *string
	ErrorMsg      *string
	DurationMs    int32
	CreatedAt     time.Time
}

// CreateTransactionLog 创建事务日志请求
type CreateTransactionLog struct {
	TransactionID string
	Operation     string
	Status        int32
	UserID        *int64
	BucketID      *int64
	ObjectKey     *string
	ErrorMsg      *string
	DurationMs    int32
}

// UpdateTransactionLogStatus 更新事务日志状态请求
type UpdateTransactionLogStatus struct {
	Status     int32
	ErrorMsg   *string
	DurationMs int32
}
