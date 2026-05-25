package dto

type ListAsyncTasksReq struct {
	TaskType string `form:"task_type,omitempty"`
	BizType  string `form:"biz_type,omitempty"`
	BizID    string `form:"biz_id,omitempty"`
	Status   *int32 `form:"status,omitempty"`
	MarkerID int64  `form:"marker_id,omitempty"`
	Limit    int    `form:"limit,omitempty"`
}

type AsyncTaskItem struct {
	ID            int64  `json:"id"`
	UserID        int64  `json:"user_id"`
	TaskType      string `json:"task_type"`
	BizType       string `json:"biz_type"`
	BizID         string `json:"biz_id"`
	Status        int32  `json:"status"`
	Progress      int32  `json:"progress"`
	Result        string `json:"result,omitempty"`
	LastError     string `json:"last_error,omitempty"`
	RetryCount    int32  `json:"retry_count"`
	MaxRetry      int32  `json:"max_retry"`
	DurationMs    int64  `json:"duration_ms"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
	NextRetryable bool   `json:"next_retryable"`
	Cancelable    bool   `json:"cancelable"`
}

type ListAsyncTasksResp struct {
	Items       []*AsyncTaskItem `json:"items"`
	NextMarker  int64            `json:"next_marker,omitempty"`
	IsTruncated bool             `json:"is_truncated"`
	Limit       int              `json:"limit"`
}

type AsyncTaskResp struct {
	Task *AsyncTaskItem `json:"task"`
}
