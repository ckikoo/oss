package dto

type CreateVideoPlayTokenReq struct {
	BucketName string `json:"bucket_name" validate:"required"`
	ObjectKey  string `json:"object_key" validate:"required"`
	VersionID  string `json:"version_id,omitempty"`
	ExpiresIn  int64  `json:"expires_in,omitempty"`
}

type CreateVideoPlayTokenResp struct {
	Token       string   `json:"token,omitempty"`
	PlayURL     string   `json:"play_url,omitempty"`
	ExpiresAt   int64    `json:"expires_at,omitempty"`
	Status      int32    `json:"status"`
	TranscodeID int64    `json:"transcode_id,omitempty"`
	Profiles    []string `json:"profiles"`
}

type VideoProfileStatus struct {
	Profile      string `json:"profile"`
	Status       int32  `json:"status"`
	Width        int32  `json:"width"`
	Height       int32  `json:"height"`
	Size         int64  `json:"size"`
	SegmentCount int32  `json:"segment_count"`
	DurationMs   int64  `json:"duration_ms"`
	LastError    string `json:"last_error,omitempty"`
}

type GetVideoTranscodeStatusResp struct {
	ObjectKey   string                `json:"object_key"`
	VersionID   string                `json:"version_id"`
	Status      int32                 `json:"status"`
	DurationMs  int64                 `json:"duration_ms"`
	DerivedSize int64                 `json:"derived_size"`
	TranscodeID int64                 `json:"transcode_id,omitempty"`
	Profiles    []*VideoProfileStatus `json:"profiles"`
}

type VideoPlayToken struct {
	Token       string
	UserID      int64
	BucketID    int64
	BucketName  string
	ObjectID    int64
	ObjectKey   string
	VersionID   string
	TranscodeID int64
	ExpiresAt   int64
	Action      string
}
