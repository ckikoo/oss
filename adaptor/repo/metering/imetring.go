package metering

import (
	"context"
	"oss/adaptor/repo/model"
	"oss/adaptor/tx"
	"time"
)

// This package contains repository logic for daily metering statistics.
type IMeteringRepo interface {
	WithTx(tx tx.Tx) IMeteringRepo
	UpdateDailyMetrics(ctx context.Context, userID int64, bucketID *int64, statDate time.Time, deltaStorageSize, deltaObjectCount, deltaUploadFlow, deltaDownloadFlow, deltaGetRequestCount, deltaPutRequestCount, deltaDelRequestCount int64) error
	ListDailyMetrics(ctx context.Context, userID int64, bucketID int64, hasBucketID bool, dateFrom, dateTo *time.Time) ([]*model.MeteringDaily, error)
}
