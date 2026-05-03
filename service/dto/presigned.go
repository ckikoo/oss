package dto

type CreatePresignedUrlReq struct {
	BucketName string `json:"bucket_name" form:"bucket_name" validate:"required"`
	ObjectKey  string `json:"object_key" form:"object_key" validate:"required"`
	Method     string `json:"method" form:"method" validate:"required"`
	ExpiresIn  int64  `json:"expires_in" form:"expires_in" validate:"required"`
	SingleUse  int32  `json:"single_use" form:"single_use"`
}

type CreatePresignedUrlResp struct {
	Token     string `json:"token"`
	URL       string `json:"url"`
	ExpiresAt int64  `json:"expires_at"`
	Method    string `json:"method"`
	SingleUse int32  `json:"single_use"`
}

type RevokePresignedUrlResp struct {
	Success bool `json:"success"`
}
