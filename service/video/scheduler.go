package video

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"

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
	SourcePath    string
}

type transcodeTaskRef struct {
	taskID      int64
	profileID   int64
	transcodeID int64
}

type videoMeta struct {
	width  int
	height int
	fps    int
}

func NewScheduler(adaptor adaptor.IAdaptor) *Scheduler {
	return &Scheduler{
		txManager:  adaptor.GetTxManager(),
		videoRepo:  gormVideo.NewVideoRepo(adaptor),
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

	meta, err := getVideoMeta(source.SourcePath)
	if err != nil {
		return fmt.Errorf("schedule Transcode error: %v", err)
	}

	defaultProfiles := AvailableProfiles(meta.height)

	taskRefs := make([]transcodeTaskRef, 0, len(defaultProfiles))
	err = s.txManager.RunInTx(ctx, func(ctx context.Context, tx tx.Tx) error {
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

		profiles, err := videoRepo.CreateProfiles(ctx, transcode.ID, buildDefaultProfileCreates(defaultProfiles, meta))
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

	if source.SourcePath == "" {
		return fmt.Errorf("source_path is required")
	}

	return nil
}

// AvailableProfiles 根据源视频高度过滤可用档位
func AvailableProfiles(srcHeight int) []consts.VideoTranscodeProfile {
	var profiles []consts.VideoTranscodeProfile
	for _, p := range consts.DefaultVideoTranscodeProfiles() {
		if int(p.Height) <= srcHeight {
			profiles = append(profiles, p)
		} else if float64(p.Height) <= float64(srcHeight)*1.1 {
			// 接近标准档位，用源分辨率代替，避免上采样
			profiles = append(profiles, consts.VideoTranscodeProfile{
				Profile:      p.Profile,
				Height:       int32(srcHeight),
				VideoBitrate: p.VideoBitrate,
				AudioBitrate: p.AudioBitrate,
			})
			break
		}
	}

	// 兜底：至少保留一个档位
	if len(profiles) == 0 {
		profiles = append(profiles, consts.VideoTranscodeProfile{
			Height:       int32(srcHeight),
			VideoBitrate: "400k",
			AudioBitrate: "64k",
		})
	}

	return profiles
}

// getVideoMeta 通过 ffprobe 获取源视频宽高和帧率，本地文件通常 100~500ms。
func getVideoMeta(inputPath string) (*videoMeta, error) {
	out, err := exec.Command("ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height,r_frame_rate",
		"-of", "json",
		inputPath,
	).Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed: %w", err)
	}
	return parseVideoMeta(out)
}

func parseVideoMeta(out []byte) (*videoMeta, error) {
	var parsed struct {
		Streams []struct {
			Width      int    `json:"width"`
			Height     int    `json:"height"`
			RFrameRate string `json:"r_frame_rate"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("parse ffprobe output: %w", err)
	}
	if len(parsed.Streams) == 0 {
		return nil, fmt.Errorf("ffprobe output has no video stream")
	}
	stream := parsed.Streams[0]
	if stream.Width <= 0 || stream.Height <= 0 {
		return nil, fmt.Errorf("invalid video dimensions: width=%d height=%d", stream.Width, stream.Height)
	}
	fps := parseFrameRate(stream.RFrameRate)
	if fps <= 0 {
		fps = 30
	}
	return &videoMeta{width: stream.Width, height: stream.Height, fps: fps}, nil
}

func parseFrameRate(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	fpsParts := strings.Split(raw, "/")
	if len(fpsParts) == 2 {
		num, _ := strconv.Atoi(fpsParts[0])
		den, _ := strconv.Atoi(fpsParts[1])
		if den > 0 {
			return int(math.Round(float64(num) / float64(den)))
		}
	}
	fps, _ := strconv.Atoi(raw)
	return fps
}

func buildDefaultProfileCreates(defaultProfiles []consts.VideoTranscodeProfile, meta *videoMeta) []*do.CreateVideoProfile {
	if meta == nil {
		meta = &videoMeta{fps: 30}
	}
	profiles := make([]*do.CreateVideoProfile, 0, len(defaultProfiles))
	for _, profile := range defaultProfiles {
		width, height := targetProfileDimensions(meta.width, meta.height, int(profile.Height))
		profiles = append(profiles, &do.CreateVideoProfile{
			Fps:          int32(meta.fps),
			Profile:      profile.Profile,
			Status:       consts.TranscodeStatusPending,
			VideoBitrate: profile.VideoBitrate,
			AudioBitrate: profile.AudioBitrate,
			Width:        width,
			Height:       height,
		})
	}
	return profiles
}

func targetProfileDimensions(srcWidth int, srcHeight int, targetHeight int) (int32, int32) {
	if targetHeight <= 0 {
		return int32(srcWidth), int32(srcHeight)
	}
	if srcWidth <= 0 || srcHeight <= 0 {
		return 0, int32(targetHeight)
	}
	if targetHeight > srcHeight {
		targetHeight = srcHeight
	}
	width := int(math.Round(float64(srcWidth) * float64(targetHeight) / float64(srcHeight)))
	if width%2 != 0 {
		width++
	}
	return int32(width), int32(targetHeight)
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
