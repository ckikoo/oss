package video

import (
	"context"
	"fmt"
	"strconv"

	"oss/adaptor"
	"oss/adaptor/redis"
	"oss/adaptor/repo/async"
	gormAsync "oss/adaptor/repo/async/gorm"
	videoRepo "oss/adaptor/repo/video"
	gormVideo "oss/adaptor/repo/video/gorm"
	"oss/adaptor/tx"
	"oss/consts"
	"oss/service/do"
	"oss/utils/logger"

	"go.uber.org/zap"
)

type Scheduler struct {
	txManager  tx.ITxManager
	videoRepo  videoRepo.IVideoRepo
	asyncRepo  async.IAsyncTaskRepo
	asyncRedis redis.ITask
	logger     *zap.Logger
}

type TranscodeSource struct {
	UserID        int64
	BucketID      int64
	BucketName    string
	ObjectID      int64
	ObjectKey     string
	ObjectKeyHash string
	VersionID     string
	SourceEtag    string
	SourceSize    int64
	ContentType   string
}

type transcodeTaskRef struct {
	taskID      int64
	profileID   int64
	transcodeID int64
}

func NewScheduler(adaptor adaptor.IAdaptor) *Scheduler {
	return &Scheduler{
		txManager:  adaptor.GetTxManager(),
		videoRepo:  gormVideo.NewVideoRepo(adaptor.GetGORM()),
		asyncRepo:  gormAsync.NewAsyncTaskRepo(adaptor.GetGORM()),
		asyncRedis: redis.NewTask(adaptor),
		logger:     logger.GetLogger().With(zap.String("module", "video_scheduler")),
	}
}

func (s *Scheduler) ScheduleTranscode(ctx context.Context, source *TranscodeSource) error {
	if source == nil {
		return nil
	}
	if !consts.IsVideoObject(source.ContentType, source.ObjectKey) {
		return nil
	}
	if err := validateTranscodeSource(source); err != nil {
		return err
	}

	defaultProfiles := consts.DefaultVideoTranscodeProfiles()
	taskRefs := make([]transcodeTaskRef, 0, len(defaultProfiles))
	err := s.txManager.RunInTx(ctx, func(ctx context.Context, tx tx.Tx) error {
		videoRepo := s.videoRepo.WithTx(tx)
		asyncRepo := s.asyncRepo.WithTx(tx)

		transcode, err := videoRepo.CreateTranscode(ctx, &do.CreateVideoTranscode{
			UserID:        source.UserID,
			BucketID:      source.BucketID,
			BucketName:    source.BucketName,
			ObjectID:      source.ObjectID,
			ObjectKey:     source.ObjectKey,
			ObjectKeyHash: source.ObjectKeyHash,
			VersionID:     source.VersionID,
			SourceEtag:    source.SourceEtag,
			SourceSize:    source.SourceSize,
			Status:        consts.TranscodeStatusPending,
			ProfileCount:  int32(len(defaultProfiles)),
		})
		if err != nil {
			return fmt.Errorf("create video transcode: %w", err)
		}

		profiles, err := videoRepo.CreateProfiles(ctx, transcode.ID, buildDefaultProfileCreates(defaultProfiles))
		if err != nil {
			return fmt.Errorf("create video profiles: %w", err)
		}

		for _, profile := range profiles {
			if profile == nil || profile.ID <= 0 {
				continue
			}
			taskID, err := asyncRepo.CreateAsyncTask(ctx, &do.CreateAsyncTask{
				UserId:   source.UserID,
				TaskType: consts.TaskTypeTranscode,
				BizType:  consts.TaskBizTypeVideoProfile,
				BizID:    strconv.FormatInt(profile.ID, 10),
				Status:   consts.TaskStatusPending,
				MaxRetry: 3,
			})
			if err != nil {
				return fmt.Errorf("create transcode async task profile_id=%d: %w", profile.ID, err)
			}
			taskRefs = append(taskRefs, transcodeTaskRef{
				taskID:      taskID,
				profileID:   profile.ID,
				transcodeID: transcode.ID,
			})
		}
		return nil
	})
	if err != nil {
		return err
	}

	for _, ref := range taskRefs {
		if err := s.enqueueAsyncTask(ctx, ref.taskID); err != nil {
			s.warn("failed to enqueue transcode task, async maintenance will retry",
				zap.Int64("task_id", ref.taskID),
				zap.Int64("profile_id", ref.profileID),
				zap.Int64("transcode_id", ref.transcodeID),
				zap.Error(err))
		}
	}

	return nil
}

func validateTranscodeSource(source *TranscodeSource) error {
	if source.UserID <= 0 {
		return fmt.Errorf("user_id is required")
	}
	if source.BucketID <= 0 || source.BucketName == "" {
		return fmt.Errorf("bucket is required")
	}
	if source.ObjectID <= 0 || source.ObjectKey == "" || source.ObjectKeyHash == "" {
		return fmt.Errorf("object is required")
	}
	if source.VersionID == "" {
		return fmt.Errorf("version_id is required")
	}
	if source.SourceEtag == "" {
		return fmt.Errorf("source_etag is required")
	}
	return nil
}

func buildDefaultProfileCreates(defaultProfiles []consts.VideoTranscodeProfile) []*do.CreateVideoProfile {
	profiles := make([]*do.CreateVideoProfile, 0, len(defaultProfiles))
	for _, profile := range defaultProfiles {
		profiles = append(profiles, &do.CreateVideoProfile{
			Profile:      profile.Profile,
			Status:       consts.TranscodeStatusPending,
			VideoBitrate: profile.VideoBitrate,
			AudioBitrate: profile.AudioBitrate,
			Height:       profile.Height,
		})
	}
	return profiles
}

func (s *Scheduler) enqueueAsyncTask(ctx context.Context, taskID int64) error {
	if taskID <= 0 {
		return nil
	}

	queued, err := s.asyncRepo.MarkAsyncTaskQueued(ctx, taskID)
	if err != nil {
		return err
	}
	if !queued {
		return nil
	}

	return s.asyncRedis.EnqueueTask(ctx, taskID)
}

func (s *Scheduler) warn(msg string, fields ...zap.Field) {
	if s.logger != nil {
		s.logger.Warn(msg, fields...)
	}
}
