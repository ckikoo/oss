package dto

import "io"

type ObjectItem struct {
	ID           int64  `json:"id"`
	ParentID     *int64 `json:"parent_id,omitempty"`
	ObjectKey    string `json:"object_key"`
	Size         int64  `json:"size"`
	Etag         string `json:"etag"`
	ContentType  string `json:"content_type,omitempty"`
	StorageClass string `json:"storage_class"`
	IsDir        int32  `json:"is_dir"`
	VersionID    string `json:"version_id,omitempty"`
	LastModified int64  `json:"last_modified"`
	Status       int32  `json:"status"`
}

type ListObjectsReq struct {
	BucketName     string `form:"bucket_name"`
	Prefix         string `form:"prefix,omitempty"`
	Delimiter      string `form:"delimiter,omitempty"`
	MaxKeys        int    `form:"max_keys,omitempty"`
	Marker         string `form:"marker,omitempty"`
	Cursor         string `form:"cursor,omitempty"`
	VersionID      string `form:"version_id,omitempty"`
	StorageClass   string `form:"storage_class,omitempty"`
	ContentType    string `form:"content_type,omitempty"`
	CreatedAtStart int64  `form:"created_at_start,omitempty"`
	CreatedAtEnd   int64  `form:"created_at_end,omitempty"`
	DirectoryOrder bool   `json:"-" form:"-"`
}

type ListObjectsResp struct {
	Items          []*ObjectItem `json:"items"`
	CommonPrefixes []string      `json:"common_prefixes,omitempty"`
	NextMarker     string        `json:"next_marker,omitempty"`
	NextCursor     string        `json:"next_cursor,omitempty"`
	IsTruncated    bool          `json:"is_truncated"`
	MaxKeys        int           `json:"max_keys"`
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
	IsLatest     int32  `json:"is_latest"`
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

type RestoreObjectVersionReq struct {
	Reason string `json:"reason,omitempty"`
}

type RestoreObjectVersionResp struct {
	ObjectKey       string `json:"object_key"`
	SourceVersionID string `json:"source_version_id"`
	VersionID       string `json:"version_id"`
	Etag            string `json:"etag"`
	Size            int64  `json:"size"`
}

type PutObjectStreamReq struct {
	BucketName    string `json:"bucket_name"`
	ObjectKey     string `json:"object_key"`
	ContentType   string `json:"content_type,omitempty"`
	StorageClass  string `json:"storage_class,omitempty"`
	Acl           int32  `json:"acl,omitempty"`
	Metadata      string `json:"metadata,omitempty"`
	UploadID      string `json:"upload_id,omitempty"`
	Overwrite     bool   `json:"overwrite"`
	CallbackUrl   string `json:"callback_url"`
	ContentLength int64  `json:"content_length"`
}

type ObjectStreamResp struct {
	ObjectKey     string        `json:"object_key"`
	Size          int64         `json:"size"`
	Etag          string        `json:"etag"`
	ContentType   string        `json:"content_type,omitempty"`
	StorageClass  string        `json:"storage_class"`
	VersionID     string        `json:"version_id,omitempty"`
	LastModified  int64         `json:"last_modified"`
	Body          io.ReadCloser `json:"-"`
	IsMultipart   bool          `json:"is_multipart"`
	ContentLength int64         `json:"content_length"`
}
