package dto

type CreateMultipartUploadReq struct {
	ObjectKey    string `json:"object_key" form:"object_key"`
	ContentType  string `json:"content_type,omitempty" form:"content_type"`
	StorageClass string `json:"storage_class,omitempty" form:"storage_class"`
	Acl          int32  `json:"acl,omitempty" form:"acl"`
	Metadata     string `json:"metadata,omitempty" form:"metadata"`
	TotalChunk   int32  `json:"total_chunk,omitempty" form:"total_chunk"`
}

type CreateMultipartUploadResp struct {
	UploadID   string `json:"upload_id"`
	BucketID   int64  `json:"bucket_id"`
	ObjectKey  string `json:"object_key"`
	TotalChunk int32  `json:"total_chunk"`
	Status     int32  `json:"status"`
	ExpiresAt  int64  `json:"expires_at"`
}

type UploadMultipartPartResp struct {
	PartNumber int32  `json:"part_number"`
	Etag       string `json:"etag"`
	Size       int64  `json:"size"`
	Status     int32  `json:"status"`
}

type MultipartCompletePart struct {
	PartNumber int32  `json:"part_number"`
	Etag       string `json:"etag"`
}

type CompleteMultipartUploadReq struct {
	Parts []MultipartCompletePart `json:"parts"`
}

type CompleteMultipartUploadResp struct {
	ObjectID  int64  `json:"object_id"`
	ObjectKey string `json:"object_key"`
	VersionID string `json:"version_id"`
	Status    int32  `json:"status"`
}

type AbortMultipartUploadResp struct {
	Success bool `json:"success"`
}
