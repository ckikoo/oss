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

type PutObjectReq struct {
	BucketName   string `json:"bucket_name"`
	ObjectKey    string `json:"object_key"`
	ContentType  string `json:"content_type,omitempty"`
	StorageClass string `json:"storage_class,omitempty"`
	Acl          int32  `json:"acl,omitempty"`
	Metadata     string `json:"metadata,omitempty"`
	UploadID     string `json:"upload_id,omitempty"` // 上传ID，用于验证上传权限
	// File is handled via multipart/form-data, not JSON
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
