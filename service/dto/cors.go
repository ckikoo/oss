package dto

type CreateBucketCorsRuleReq struct {
	Origin         string   `json:"origin"`
	AllowedMethods []string `json:"allowed_methods"`
	MaxAgeSeconds  int32    `json:"max_age_seconds,omitempty"`
}

type UpdateBucketCorsRuleReq struct {
	Origin         *string  `json:"origin,omitempty"`
	AllowedMethods []string `json:"allowed_methods,omitempty"`
	MaxAgeSeconds  *int32   `json:"max_age_seconds,omitempty"`
}

type BucketCorsRuleResp struct {
	ID             int64    `json:"id"`
	UserID         int64    `json:"user_id"`
	BucketName     string   `json:"bucket_name"`
	Origin         string   `json:"origin"`
	AllowedMethods []string `json:"allowed_methods"`
	MaxAgeSeconds  int32    `json:"max_age_seconds"`
	CreatedAt      int64    `json:"created_at"`
	UpdatedAt      int64    `json:"updated_at"`
}

type ListBucketCorsRulesResp struct {
	Items []*BucketCorsRuleResp `json:"items"`
}

type BucketCorsCheckResult struct {
	AllowedOrigin  string
	AllowedMethods []string
	MaxAgeSeconds  int32
}
