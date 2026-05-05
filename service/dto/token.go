package dto

type CreateUploadTokenReq struct {
	BucketName string `json:"bucket_name" validate:"required"`
	ObjectKey  string `json:"object_key"` // 上传时可选，不传则 hash 命名
	ExpiresIn  int64  `json:"expires_in"`

	// 上传专属 policy（Action=write 时生效）
	MimeLimit   string `json:"mime_limit"`   // 限制文件类型 如 image/*
	SizeLimit   int64  `json:"size_limit"`   // 限制文件大小（字节）
	Overwrite   bool   `json:"overwrite"`    // 是否允许覆盖同名文件
	CallbackUrl string `json:"callback_url"` // 上传成功回调
}

type CreateDownloadTokenReq struct {
	BucketName string `json:"bucket_name" validate:"required"`
	ObjectKey  string `json:"object_key" validate:"required"`
	ExpiresIn  int64  `json:"expires_in"`
}

type CreateTokenResp struct {
	Token    string `json:"token"`
	ExpireAt int64  `json:"expire_at"`
}
