package gorm

import (
	"context"
	"fmt"
	"time"

	"oss/adaptor/repo/metering"
	"oss/adaptor/repo/model"
	"oss/adaptor/repo/query"

	"gorm.io/gorm"
)

type MeteringRepo struct {
	db *gorm.DB
}

var _ metering.IMeteringRepo = (*MeteringRepo)(nil)

func NewMeteringRepo(db *gorm.DB) *MeteringRepo {
	return &MeteringRepo{db: db}
}

func (r *MeteringRepo) UpdateDailyMetrics(ctx context.Context, userID int64, bucketID *int64, statDate time.Time, deltaStorageSize, deltaObjectCount, deltaUploadFlow, deltaDownloadFlow, deltaGetRequestCount, deltaPutRequestCount, deltaDelRequestCount int64) error {
	if userID <= 0 {
		return fmt.Errorf("user_id must be positive")
	}
	if err := r.upsertDailyMetrics(ctx, r.db.WithContext(ctx), userID, bucketID, statDate, deltaStorageSize, deltaObjectCount, deltaUploadFlow, deltaDownloadFlow, deltaGetRequestCount, deltaPutRequestCount, deltaDelRequestCount); err != nil {
		return err
	}
	if bucketID != nil {
		return r.upsertDailyMetrics(ctx, r.db.WithContext(ctx), userID, nil, statDate, deltaStorageSize, deltaObjectCount, deltaUploadFlow, deltaDownloadFlow, deltaGetRequestCount, deltaPutRequestCount, deltaDelRequestCount)
	}
	return nil
}

func (r *MeteringRepo) UpdateDailyMetricsWithTx(tx *gorm.DB, ctx context.Context, userID int64, bucketID *int64, statDate time.Time, deltaStorageSize, deltaObjectCount, deltaUploadFlow, deltaDownloadFlow, deltaGetRequestCount, deltaPutRequestCount, deltaDelRequestCount int64) error {
	if userID <= 0 {
		return fmt.Errorf("user_id must be positive")
	}
	if err := r.upsertDailyMetrics(ctx, tx.WithContext(ctx), userID, bucketID, statDate, deltaStorageSize, deltaObjectCount, deltaUploadFlow, deltaDownloadFlow, deltaGetRequestCount, deltaPutRequestCount, deltaDelRequestCount); err != nil {
		return err
	}
	if bucketID != nil {
		return r.upsertDailyMetrics(ctx, tx.WithContext(ctx), userID, nil, statDate, deltaStorageSize, deltaObjectCount, deltaUploadFlow, deltaDownloadFlow, deltaGetRequestCount, deltaPutRequestCount, deltaDelRequestCount)
	}
	return nil
}

func (r *MeteringRepo) upsertDailyMetrics(ctx context.Context, db *gorm.DB, userID int64, bucketID *int64, statDate time.Time, deltaStorageSize, deltaObjectCount, deltaUploadFlow, deltaDownloadFlow, deltaGetRequestCount, deltaPutRequestCount, deltaDelRequestCount int64) error {
	sql := `INSERT INTO metering_daily
        (user_id, bucket_id, stat_date, storage_size, object_count, upload_flow, download_flow, get_request_count, put_request_count, del_request_count)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON DUPLICATE KEY UPDATE
            storage_size = storage_size + VALUES(storage_size),
            object_count = object_count + VALUES(object_count),
            upload_flow = upload_flow + VALUES(upload_flow),
            download_flow = download_flow + VALUES(download_flow),
            get_request_count = get_request_count + VALUES(get_request_count),
            put_request_count = put_request_count + VALUES(put_request_count),
            del_request_count = del_request_count + VALUES(del_request_count)`

	result := db.Exec(sql,
		userID,
		bucketID,
		statDate.Format("2006-01-02"),
		deltaStorageSize,
		deltaObjectCount,
		deltaUploadFlow,
		deltaDownloadFlow,
		deltaGetRequestCount,
		deltaPutRequestCount,
		deltaDelRequestCount,
	)
	return result.Error
}

func (r *MeteringRepo) ListDailyMetrics(ctx context.Context, userID int64, bucketID int64, hasBucketID bool, dateFrom, dateTo *time.Time) ([]*model.MeteringDaily, error) {
	q := query.Use(r.db)
	qs := q.MeteringDaily.WithContext(ctx)
	if userID > 0 {
		qs = qs.Where(q.MeteringDaily.UserID.Eq(userID))
	}
	if hasBucketID {
		if bucketID > 0 {
			qs = qs.Where(q.MeteringDaily.BucketID.Eq(bucketID))
		} else {
			qs = qs.Where(q.MeteringDaily.BucketID.IsNull())
		}
	}
	if dateFrom != nil {
		qs = qs.Where(q.MeteringDaily.StatDate.Gte(*dateFrom))
	}
	if dateTo != nil {
		qs = qs.Where(q.MeteringDaily.StatDate.Lte(*dateTo))
	}
	qs = qs.Order(q.MeteringDaily.StatDate)
	return qs.Find()
}
