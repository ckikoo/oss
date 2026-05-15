package do

import "time"

type BucketCorsRuleDo struct {
	ID             int64
	UserID         int64
	BucketName     string
	Origin         string
	AllowedMethods []string
	MaxAgeSeconds  int32
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type CreateBucketCorsRule struct {
	UserID         int64
	BucketName     string
	Origin         string
	AllowedMethods []string
	MaxAgeSeconds  int32
}

type UpdateBucketCorsRule struct {
	Origin         *string
	AllowedMethods []string
	MaxAgeSeconds  *int32
}
