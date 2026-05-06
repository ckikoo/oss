package dto

type BucketItem struct {
	ID           int64  `json:"id"`
	UserID       int64  `json:"user_id"`
	Name         string `json:"name"`
	Region       string `json:"region"`
	Acl          int32  `json:"acl"`
	Versioning   int32  `json:"versioning"`
	Status       int32  `json:"status"`
	StorageClass string `json:"storage_class"`
	ObjectCount  int64  `json:"object_count"`
	StorageSize  int64  `json:"storage_size"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
}

type CreateBucketReq struct {
	UserID       int64  `json:"user_id"`
	Name         string `json:"name"`
	Region       string `json:"region,omitempty"`
	Acl          int32  `json:"acl,omitempty"`
	Versioning   int32  `json:"versioning,omitempty"`
	StorageClass string `json:"storage_class,omitempty"`
}

type CreateBucketResp struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Region       string `json:"region"`
	Acl          int32  `json:"acl"`
	Versioning   int32  `json:"versioning"`
	Status       int32  `json:"status"`
	StorageClass string `json:"storage_class"`
	ObjectCount  int64  `json:"object_count"`
	StorageSize  int64  `json:"storage_size"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
}

type ListBucketsReq struct {
	Status int32 `form:"status"`
}

type ListBucketsResp struct {
	Items []*BucketItem `json:"items"`
}

type UpdateBucketReq struct {
	Acl          *int32 `json:"acl,omitempty"`
	Versioning   *int32 `json:"versioning,omitempty"`
	Status       *int32 `json:"status,omitempty"`
	StorageClass string `json:"storage_class,omitempty"`
}

type UpdateBucketResp struct {
	ID           int64  `json:"id"`
	UserID       int64  `json:"user_id"`
	Name         string `json:"name"`
	Region       string `json:"region"`
	Acl          int32  `json:"acl"`
	Versioning   int32  `json:"versioning"`
	Status       int32  `json:"status"`
	StorageClass string `json:"storage_class"`
	ObjectCount  int64  `json:"object_count"`
	StorageSize  int64  `json:"storage_size"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
}
