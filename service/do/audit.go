package do

import (
	"oss/common"
	"time"
)

type OperationLogFilter struct {
	UserID     int64
	BucketName string
	Action     string
	Status     *int32
	DateFrom   *time.Time
	DateTo     *time.Time
	common.Pager
}

type OperationLogDo struct {
	ID        int64
	RequestID string
	UserID    *int64
	Action    string
	Result    int32
	ClientIP  *string
	Duration  int32
	CreatedAt time.Time
}

type CreateOperationLog struct {
	RequestID    string
	UserID       *int64
	AccessKey    *string
	BucketID     *int64
	BucketName   *string
	ObjectKey    *string
	Action       string
	Result       int32
	StatusCode   int32
	ErrorCode    *string
	ClientIP     *string
	UserAgent    *string
	RequestSize  int64
	ResponseSize int64
	DurationMs   int32
}
