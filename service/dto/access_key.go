package dto

type AccessKeyItem struct {
	ID         int64  `json:"id"`
	AccessKey  string `json:"access_key"`
	Alias      string `json:"alias,omitempty"`
	Status     int32  `json:"status"`
	UserID     int64  `json:"user_id"`
	Permission string `json:"permission,omitempty"`
	ExpiresAt  int64  `json:"expires_at,omitempty"`
	LastUsedAt int64  `json:"last_used_at,omitempty"`
}

type CreateAccessKeyReq struct {
	UserID     int64  `json:"user_id"`
	Alias      string `json:"alias,omitempty"`
	Permission string `json:"permission,omitempty"`
	ExpiresAt  int64  `json:"expires_at,omitempty"`
}

type CreateAccessKeyResp struct {
	Id         int64  `json:"id"`
	AccessKey  string `json:"access_key"`
	SecretKey  string `json:"secret_key"`
	Alias      string `json:"alias,omitempty"`
	Status     int32  `json:"status"`
	Permission string `json:"permission,omitempty"`
	ExpiresAt  int64  `json:"expires_at,omitempty"`
}

type ListAccessKeysReq struct {
	UserID int64 `json:"-"`
	Status int32 `json:"status"`
}

type ListAccessKeysResp struct {
	Items []*AccessKeyItem `json:"items"`
}

type UpdateAccessKeyStatusReq struct {
	Status int32 `json:"status"`
}

type UpdateAccessKeyStatusResp struct {
	ID        int64  `json:"id"`
	AccessKey string `json:"access_key"`
	Status    int32  `json:"status"`
}
