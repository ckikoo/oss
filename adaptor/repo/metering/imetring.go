package metering

import (
	"context"
	"oss/adaptor/repo/model"
	"time"

	"gorm.io/gorm"
)

// This package contains repository logic for daily metering statistics.
type IMeteringRepo interface {
	UpdateDailyMetrics(ctx context.Context, userID int64, bucketID *int64, statDate time.Time, deltaStorageSize, deltaObjectCount, deltaUploadFlow, deltaDownloadFlow, deltaGetRequestCount, deltaPutRequestCount, deltaDelRequestCount int64) error
	UpdateDailyMetricsWithTx(tx *gorm.DB, ctx context.Context, userID int64, bucketID *int64, statDate time.Time, deltaStorageSize, deltaObjectCount, deltaUploadFlow, deltaDownloadFlow, deltaGetRequestCount, deltaPutRequestCount, deltaDelRequestCount int64) error
	ListDailyMetrics(ctx context.Context, userID int64, bucketID int64, hasBucketID bool, dateFrom, dateTo *time.Time) ([]*model.MeteringDaily, error)
}
