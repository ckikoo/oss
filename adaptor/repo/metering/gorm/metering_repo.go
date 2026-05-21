package gorm

import (
	"context"
	"fmt"
	"time"

	"oss/adaptor/repo/metering"
	"oss/adaptor/repo/model"
	"oss/adaptor/repo/query"
	"oss/adaptor/repo/repoerr"
	"oss/adaptor/tx"

	"gorm.io/gorm"
)

type MeteringRepo struct {
	db *gorm.DB
	q  *query.Query
}

var _ metering.IMeteringRepo = (*MeteringRepo)(nil)

func NewMeteringRepo(db *gorm.DB) *MeteringRepo {
	return &MeteringRepo{db: db, q: query.Use(db)}
}

func (r *MeteringRepo) WithTx(tx tx.Tx) metering.IMeteringRepo {
	return &MeteringRepo{db: tx.(*gorm.DB), q: query.Use(tx.(*gorm.DB))}
}

func (r *MeteringRepo) UpdateDailyMetrics(ctx context.Context, userID int64, bucketID *int64, statDate time.Time, deltaStorageSize, deltaObjectCount, deltaUploadFlow, deltaDownloadFlow, deltaGetRequestCount, deltaPutRequestCount, deltaDelRequestCount int64) error {
	if userID <= 0 {
		return fmt.Errorf("user_id must be positive")
	}
	if err := r.upsertDailyMetrics(ctx, r.db.WithContext(ctx), userID, bucketID, statDate, deltaStorageSize, deltaObjectCount, deltaUploadFlow, deltaDownloadFlow, deltaGetRequestCount, deltaPutRequestCount, deltaDelRequestCount); err != nil {
		return repoerr.Wrap(err)
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
		return repoerr.Wrap(err)
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
	return repoerr.Wrap(result.Error)
}

func (r *MeteringRepo) ListDailyMetrics(ctx context.Context, userID int64, bucketID int64, hasBucketID bool, dateFrom, dateTo *time.Time) ([]*model.MeteringDaily, error) {
	q := r.q.MeteringDaily
	qs := q.WithContext(ctx)
	if userID > 0 {
		qs = qs.Where(q.UserID.Eq(userID))
	}
	if hasBucketID {
		if bucketID > 0 {
			qs = qs.Where(q.BucketID.Eq(bucketID))
		} else {
			qs = qs.Where(q.BucketID.IsNull())
		}
	}
	if dateFrom != nil {
		qs = qs.Where(q.StatDate.Gte(*dateFrom))
	}
	if dateTo != nil {
		qs = qs.Where(q.StatDate.Lte(*dateTo))
	}
	qs = qs.Order(q.StatDate)
	item, err := qs.Find()
	if err != nil {
		return nil, repoerr.Wrap(err)
	}
	return item, nil
}
