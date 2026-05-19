package dto

import "oss/common"

type ListOperationLogsReq struct {
	BucketName string `form:"bucket_name,omitempty"`
	Action     string `form:"action,omitempty"`
	Status     *int32 `form:"status,omitempty"`
	DateFrom   string `form:"date_from,omitempty"`
	DateTo     string `form:"date_to,omitempty"`
	common.Pager
}

type OperationLogItem struct {
	LogID     int64  `json:"log_id"`
	UserID    *int64 `json:"user_id,omitempty"`
	Action    string `json:"action"`
	Status    int32  `json:"status"`
	IP        string `json:"ip,omitempty"`
	Duration  int32  `json:"duration"`
	RequestID string `json:"request_id,omitempty"`
	Timestamp string `json:"timestamp"`
}

type ListOperationLogsResp struct {
	Total int64               `json:"total"`
	Items []*OperationLogItem `json:"items"`
}
