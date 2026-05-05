package dto

type CreatePresignedUrlReq struct {
	BucketName string `json:"bucket_name" form:"bucket_name" validate:"required"`
	ObjectKey  string `json:"object_key" form:"object_key" validate:"required"`
	ExpiresIn  int64  `json:"expires_in" form:"expires_in" validate:"required"`
}

type CreatePresignedUrlResp struct {
	URL       string `json:"url"`
	ExpiresAt int64  `json:"expires_at"`
}

type CreateSimpleUploadReq struct {
	BucketName string `json:"bucket_name" form:"bucket_name" validate:"required"`
	ObjectKey  string `json:"object_key" form:"object_key" validate:"required"`
	ExpiresIn  int64  `json:"expires_in" form:"expires_in" validate:"required"`
}
type CreateSimpleUploadResp struct {
	URL       string `json:"url"`
	ExpiresAt int64  `json:"expires_at"`
}
