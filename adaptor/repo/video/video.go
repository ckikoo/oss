package video

import (
	"context"

	"oss/adaptor/tx"
	"oss/service/do"
)

type IVideoRepo interface {
	WithTx(tx tx.Tx) IVideoRepo
	CreateTranscode(ctx context.Context, in *do.CreateVideoTranscode) (*do.VideoTranscodeDo, error)
	GetTranscodeByObjectVersion(ctx context.Context, objectID int64, versionID string) (*do.VideoTranscodeDo, error)
	GetTranscodeByID(ctx context.Context, transcodeID int64) (*do.VideoTranscodeDo, error)
	UpdateTranscode(ctx context.Context, transcodeID int64, in *do.UpdateVideoTranscode) error
	MarkTranscodeDeleted(ctx context.Context, transcodeID int64) error
	CreateProfiles(ctx context.Context, transcodeID int64, profiles []*do.CreateVideoProfile) ([]*do.VideoProfileDo, error)
	GetProfileByID(ctx context.Context, profileID int64) (*do.VideoProfileDo, error)
	ListProfiles(ctx context.Context, transcodeID int64) ([]*do.VideoProfileDo, error)
	ListDoneProfiles(ctx context.Context, transcodeID int64) ([]*do.VideoProfileDo, error)
	UpdateProfile(ctx context.Context, profileID int64, in *do.UpdateVideoProfile) error
	SaveEncryptKey(ctx context.Context, in *do.CreateVideoEncryptKey) error
	GetEncryptKeyByKeyID(ctx context.Context, keyID string) (*do.VideoEncryptKeyDo, error)
	GetEncryptKeyByProfileID(ctx context.Context, profileID int64) (*do.VideoEncryptKeyDo, error)
}
