package audit

import (
	"time"

	"oss/adaptor"
	auditRepo "oss/adaptor/repo/audit"
	"oss/common"
	"oss/consts"
	"oss/service/do"
	"oss/service/dto"
)

type Service struct {
	repo auditRepo.IOperationLogRepo
}

func NewService(adaptor adaptor.IAdaptor) *Service {
	return &Service{repo: auditRepo.NewOperationLogRepo(adaptor.GetGORM())}
}

func (srv *Service) ListOperationLogs(ctx *common.UserInfoCtx, req *dto.ListOperationLogsReq) (*dto.ListOperationLogsResp, common.Errno) {
	var dateFrom *time.Time
	var dateTo *time.Time
	if req.DateFrom != "" {
		parsed, err := time.Parse(consts.DateFormatYMD, req.DateFrom)
		if err != nil {
			return nil, common.ParamErr.WithMsg("invalid date_from format, expected YYYY-MM-DD")
		}
		dateFrom = &parsed
	}
	if req.DateTo != "" {
		parsed, err := time.Parse(consts.DateFormatYMD, req.DateTo)
		if err != nil {
			return nil, common.ParamErr.WithMsg("invalid date_to format, expected YYYY-MM-DD")
		}
		dateTo = &parsed
	}
	if req.Status != nil && *req.Status != consts.OperationLogResultFailed && *req.Status != consts.OperationLogResultSuccess {
		return nil, common.ParamErr.WithMsg("status must be 0 or 1")
	}

	filter := &do.OperationLogFilter{
		UserID:     ctx.UserID,
		BucketName: req.BucketName,
		Action:     req.Action,
		Status:     req.Status,
		DateFrom:   dateFrom,
		DateTo:     dateTo,
		Pager:      req.Pager,
	}

	logs, total, err := srv.repo.ListByFilter(ctx, filter)
	if err != nil {
		return nil, common.DatabaseErr.WithErr(err)
	}

	items := make([]*dto.OperationLogItem, 0, len(logs))
	for _, row := range logs {
		ip := ""
		if row.ClientIP != nil {
			ip = *row.ClientIP
		}
		items = append(items, &dto.OperationLogItem{
			LogID:     row.ID,
			UserID:    row.UserID,
			Action:    row.Action,
			Status:    row.Result,
			IP:        ip,
			Duration:  row.Duration,
			RequestID: row.RequestID,
			Timestamp: row.CreatedAt.Format(time.RFC3339),
		})
	}

	return &dto.ListOperationLogsResp{Total: total, Items: items}, common.OK
}
