package do

import "time"

type VideoTranscodeDo struct {
	ID               int64
	UserID           int64
	BucketID         int64
	BucketName       string
	ObjectID         int64
	ObjectKey        string
	ObjectKeyHash    string
	VersionID        string
	SourceEtag       string
	SourceSize       int64
	Status           int32
	DurationMs       int64
	DerivedSize      int64
	ProfileCount     int32
	DoneProfileCount int32
	LastError        *string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	FinishedAt       *time.Time
}

type CreateVideoTranscode struct {
	UserID           int64
	BucketID         int64
	BucketName       string
	ObjectID         int64
	ObjectKey        string
	ObjectKeyHash    string
	VersionID        string
	SourceEtag       string
	SourceSize       int64
	Status           int32
	DurationMs       int64
	DerivedSize      int64
	ProfileCount     int32
	DoneProfileCount int32
	LastError        *string
	FinishedAt       *time.Time
}

type UpdateVideoTranscode struct {
	Status           *int32
	DurationMs       *int64
	DerivedSize      *int64
	ProfileCount     *int32
	DoneProfileCount *int32
	LastError        *string
	FinishedAt       *time.Time
}

type VideoProfileDo struct {
	ID           int64
	TranscodeID  int64
	Profile      string
	Status       int32
	VideoBitrate string
	AudioBitrate string
	Width        int32
	Height       int32
	AssetPrefix  string
	PlaylistKey  string
	Size         int64
	SegmentCount int32
	DurationMs   int64
	LastError    *string
	StartedAt    *time.Time
	FinishedAt   *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type CreateVideoProfile struct {
	Profile      string
	Status       int32
	VideoBitrate string
	AudioBitrate string
	Width        int32
	Height       int32
	AssetPrefix  string
	PlaylistKey  string
	Size         int64
	SegmentCount int32
	DurationMs   int64
	LastError    *string
	StartedAt    *time.Time
	FinishedAt   *time.Time
}

type UpdateVideoProfile struct {
	Status       *int32
	VideoBitrate *string
	AudioBitrate *string
	Width        *int32
	Height       *int32
	AssetPrefix  *string
	PlaylistKey  *string
	Size         *int64
	SegmentCount *int32
	DurationMs   *int64
	LastError    *string
	StartedAt    *time.Time
	FinishedAt   *time.Time
}

type VideoEncryptKeyDo struct {
	ID           int64
	TranscodeID  int64
	ProfileID    int64
	KeyID        string
	EncryptedKey []byte
	Algorithm    string
	KeyVersion   string
	KmsKeyID     string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type CreateVideoEncryptKey struct {
	TranscodeID  int64
	ProfileID    int64
	KeyID        string
	EncryptedKey []byte
	Algorithm    string
	KeyVersion   string
	KmsKeyID     string
}
