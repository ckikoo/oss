package dto

type ListDailyMeteringReq struct {
	DateFrom string `form:"date_from,omitempty"`
	DateTo   string `form:"date_to,omitempty"`
	BucketID int64  `form:"bucket_id,omitempty"`
}

type MeteringDailyItem struct {
	UserID          int64  `json:"user_id"`
	BucketID        *int64 `json:"bucket_id,omitempty"`
	StatDate        string `json:"stat_date"`
	StorageSize     int64  `json:"storage_size"`
	ObjectCount     int64  `json:"object_count"`
	UploadFlow      int64  `json:"upload_flow"`
	DownloadFlow    int64  `json:"download_flow"`
	GetRequestCount int64  `json:"get_request_count"`
	PutRequestCount int64  `json:"put_request_count"`
	DelRequestCount int64  `json:"del_request_count"`
}

type ListDailyMeteringResp struct {
	Items []*MeteringDailyItem `json:"items"`
}
