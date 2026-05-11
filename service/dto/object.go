package dto

type ObjectItem struct {
	ObjectKey    string `json:"object_key"`
	Size         int64  `json:"size"`
	Etag         string `json:"etag"`
	ContentType  string `json:"content_type,omitempty"`
	StorageClass string `json:"storage_class"`
	VersionID    string `json:"version_id,omitempty"`
	LastModified int64  `json:"last_modified"`
	Status       int32  `json:"status"`
}

type ListObjectsReq struct {
	BucketName string `form:"bucket_name"`
	Prefix     string `form:"prefix,omitempty"`
	Delimiter  string `form:"delimiter,omitempty"`
	MaxKeys    int    `form:"max_keys,omitempty"`
	Marker     string `form:"marker,omitempty"`
	VersionID  string `form:"version_id,omitempty"`
}

type ListObjectsResp struct {
	Items []*ObjectItem `json:"items"`
}

type ObjectMetadata struct {
	ObjectKey    string `json:"object_key"`
	Size         int64  `json:"size"`
	Etag         string `json:"etag"`
	ContentType  string `json:"content_type,omitempty"`
	StorageClass string `json:"storage_class"`
	VersionID    string `json:"version_id,omitempty"`
	Acl          int32  `json:"acl"`
	Metadata     string `json:"metadata,omitempty"`
	Status       int32  `json:"status"`
}

type GetObjectVersionsResp struct {
	Items []*ObjectMetadata `json:"items"`
}

type PutObjectReq struct {
	BucketName   string `json:"bucket_name"`
	ObjectKey    string `json:"object_key"`
	ContentType  string `json:"content_type,omitempty"`
	StorageClass string `json:"storage_class,omitempty"`
	Acl          int32  `json:"acl,omitempty"`
	Metadata     string `json:"metadata,omitempty"`
	UploadID     string `json:"upload_id,omitempty"` // 临时token 时产生的token，但是在这边可有可无
	MimeLimit    string `json:"mime_limit"`          // 限制文件类型 如 image/*
	Overwrite    bool   `json:"overwrite"`           // 是否允许覆盖同名文件
	CallbackUrl  string `json:"callback_url"`        // 上传成功回调
}

type PutObjectResp struct {
	ObjectKey   string `json:"object_key"`
	Size        int64  `json:"size"`
	Etag        string `json:"etag"`
	StoragePath string `json:"storage_path,omitempty"`
	VersionID   string `json:"version_id,omitempty"`
}

type DeleteObjectReq struct {
	BucketName string `json:"bucket_name"`
	ObjectKey  string `json:"object_key"`
	VersionID  string `json:"version_id,omitempty"`
}

type DeleteObjectResp struct {
	Success bool `json:"success"`
}
