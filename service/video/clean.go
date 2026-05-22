package video

import (
	"context"
	"errors"
	"fmt"

	"oss/adaptor"
	"oss/adaptor/redis"
	"oss/adaptor/repo/repoerr"
	videoRepo "oss/adaptor/repo/video"
	gormVideo "oss/adaptor/repo/video/gorm"
	"oss/adaptor/storage"
	"oss/adaptor/tx"
	"oss/consts"
	"oss/service/do"
	"oss/utils/logger"

	"go.uber.org/zap"
)

type ObjectVersionCleanup struct {
	ObjectID    int64
	VersionID   string
	BucketName  string
	TranscodeID int64
	DerivedSize int64
	AssetPrefix string
}

type CleanupService struct {
	videoRepo videoRepo.IVideoRepo
	playToken redis.IPlayToken
	storage   storage.IStorage
	logger    *zap.Logger
}

func NewCleanupService(adaptor adaptor.IAdaptor) *CleanupService {
	return &CleanupService{
		videoRepo: gormVideo.NewVideoRepo(adaptor),
		playToken: redis.NewPlayToken(adaptor),
		storage:   adaptor.GetStorage(),
		logger:    logger.GetLogger().With(zap.String("module", "video_cleanup")),
	}
}

func (s *CleanupService) PlanObjectVersionCleanup(ctx context.Context, obj *do.ObjectDo) (*ObjectVersionCleanup, error) {
	if obj == nil || obj.ID <= 0 || obj.VersionID == "" {
		return nil, nil
	}
	transcode, err := s.videoRepo.GetTranscodeByObjectVersion(ctx, obj.ID, obj.VersionID)
	if err != nil {
		if errors.Is(err, repoerr.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if transcode == nil || transcode.Status == consts.TranscodeStatusDeleted {
		return nil, nil
	}
	return &ObjectVersionCleanup{
		ObjectID:    obj.ID,
		VersionID:   obj.VersionID,
		BucketName:  transcode.BucketName,
		TranscodeID: transcode.ID,
		DerivedSize: transcode.DerivedSize,
		AssetPrefix: fmt.Sprintf("%s/%d", consts.HLSAssetPrefix, transcode.ID),
	}, nil
}

func (s *CleanupService) MarkDeletedInTx(ctx context.Context, tx tx.Tx, plan *ObjectVersionCleanup) error {
	if plan == nil || plan.TranscodeID <= 0 {
		return nil
	}
	repo := s.videoRepo.WithTx(tx)
	if err := repo.MarkTranscodeDeleted(ctx, plan.TranscodeID); err != nil {
		return err
	}
	if err := repo.MarkProfilesDeleted(ctx, plan.TranscodeID); err != nil {
		return err
	}
	return repo.DeleteEncryptKeysByTranscodeID(ctx, plan.TranscodeID)
}

func (s *CleanupService) AfterCommit(ctx context.Context, plan *ObjectVersionCleanup) {
	if plan == nil {
		return
	}
	if plan.BucketName != "" && plan.AssetPrefix != "" {
		if err := s.storage.DeleteAssetPrefix(ctx, plan.BucketName, plan.AssetPrefix); err != nil {
			s.logger.Warn("failed to delete video assets",
				zap.String("bucket_name", plan.BucketName),
				zap.String("asset_prefix", plan.AssetPrefix),
				zap.Error(err))
		}
	}
	if err := s.playToken.DeletePlayTokensByObjectVersion(ctx, plan.ObjectID, plan.VersionID); err != nil {
		s.logger.Warn("failed to invalidate video play tokens",
			zap.Int64("object_id", plan.ObjectID),
			zap.String("version_id", plan.VersionID),
			zap.Error(err))
	}
}

func (s *CleanupService) InvalidateObjectVersionTokens(ctx context.Context, obj *do.ObjectDo) {
	if obj == nil || obj.ID <= 0 || obj.VersionID == "" {
		return
	}
	if err := s.playToken.DeletePlayTokensByObjectVersion(ctx, obj.ID, obj.VersionID); err != nil {
		s.logger.Warn("failed to invalidate video play tokens",
			zap.Int64("object_id", obj.ID),
			zap.String("version_id", obj.VersionID),
			zap.Error(err))
	}
}
