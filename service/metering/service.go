package metering

import (
	"time"

	"oss/adaptor"
	"oss/adaptor/repo/metering"
	"oss/common"
	"oss/service/dto"
)

type Service struct {
	repo *metering.MeteringRepo
}

func NewService(adaptor adaptor.IAdaptor) *Service {
	return &Service{repo: metering.NewMeteringRepo(adaptor.GetGORM())}
}

func (srv *Service) ListDailyMetrics(ctx *common.UserInfoCtx, req *dto.ListDailyMeteringReq) (*dto.ListDailyMeteringResp, common.Errno) {
	var dateFrom *time.Time
	var dateTo *time.Time
	if req.DateFrom != "" {
		parsed, err := time.Parse("2006-01-02", req.DateFrom)
		if err != nil {
			return nil, common.ParamErr.WithMsg("invalid date_from format, expected YYYY-MM-DD")
		}
		dateFrom = &parsed
	}
	if req.DateTo != "" {
		parsed, err := time.Parse("2006-01-02", req.DateTo)
		if err != nil {
			return nil, common.ParamErr.WithMsg("invalid date_to format, expected YYYY-MM-DD")
		}
		dateTo = &parsed
	}

	hasBucketID := req.BucketID != 0
	results, err := srv.repo.ListDailyMetrics(ctx, ctx.UserID, req.BucketID, hasBucketID, dateFrom, dateTo)
	if err != nil {
		return nil, common.DatabaseErr.WithErr(err)
	}

	items := make([]*dto.MeteringDailyItem, 0, len(results))
	for _, row := range results {
		var bucketID *int64
		if row.BucketID != nil {
			bucketID = row.BucketID
		}
		items = append(items, &dto.MeteringDailyItem{
			UserID:          row.UserID,
			BucketID:        bucketID,
			StatDate:        row.StatDate.Format("2006-01-02"),
			StorageSize:     row.StorageSize,
			ObjectCount:     row.ObjectCount,
			UploadFlow:      row.UploadFlow,
			DownloadFlow:    row.DownloadFlow,
			GetRequestCount: row.GetRequestCount,
			PutRequestCount: row.PutRequestCount,
			DelRequestCount: row.DelRequestCount,
		})
	}

	return &dto.ListDailyMeteringResp{Items: items}, common.OK
}
