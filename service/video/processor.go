package video

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"oss/adaptor"
	"oss/adaptor/repo/admin"
	gormAdmin "oss/adaptor/repo/admin/gorm"
	"oss/adaptor/repo/bucket"
	gormBucket "oss/adaptor/repo/bucket/gorm"
	"oss/adaptor/repo/metering"
	gormMetering "oss/adaptor/repo/metering/gorm"
	"oss/adaptor/repo/multipart"
	gormMultipart "oss/adaptor/repo/multipart/gorm"
	"oss/adaptor/repo/object"
	gormObject "oss/adaptor/repo/object/gorm"
	"oss/adaptor/repo/repoerr"
	videoRepo "oss/adaptor/repo/video"
	gormVideo "oss/adaptor/repo/video/gorm"
	"oss/adaptor/storage"
	"oss/adaptor/tx"
	"oss/config"
	"oss/consts"
	"oss/service/do"
	"oss/utils/logger"
	"oss/utils/tools"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"
)

const (
	hlsPlaylistName = "index.m3u8"
	hlsSegmentGlob  = "seg_%06d.ts"
	maxFFmpegOutput = 4 * 1024
)

type Processor struct {
	txManager              tx.ITxManager
	videoRepo              videoRepo.IVideoRepo
	objectRepo             object.IObjectRepo
	multipartRepo          multipart.IMultipartRepo
	userRepo               admin.IUser
	bucketRepo             bucket.IBucketRepo
	meteringRepo           metering.IMeteringRepo
	storage                storage.IStorage
	security               config.Security
	segmentDurationSeconds int
	transcodeLimiter       *semaphore.Weighted
	ffmpegRunner           ffmpegRunner
	logger                 *zap.Logger
}

type transcodeContext struct {
	taskID      int64
	profileID   int64
	profileName string
	transcodeID int64
	objectID    int64
	versionID   string
	bucketName  string
	stagingKey  string
	finalKey    string
}

type profileKey struct {
	keyID string
	raw   []byte
}

type uploadStats struct {
	size         int64
	segmentCount int32
}

type ffmpegRunner interface {
	Run(ctx context.Context, args []string) (string, error)
}

type commandFFmpegRunner struct{}

func NewProcessor(adaptor adaptor.IAdaptor) *Processor {
	cfg := adaptor.GetConfig()
	videoCfg := config.Video{}
	securityCfg := config.Security{}
	if cfg != nil {
		videoCfg = cfg.Video
		securityCfg = cfg.Security
	}

	return &Processor{
		txManager:              adaptor.GetTxManager(),
		videoRepo:              gormVideo.NewVideoRepo(adaptor.GetGORM()),
		objectRepo:             gormObject.NewObjectRepo(adaptor),
		multipartRepo:          gormMultipart.NewObjectRepo(adaptor.GetGORM()),
		userRepo:               gormAdmin.NewUserRepo(adaptor),
		bucketRepo:             gormBucket.NewBucketRepo(adaptor),
		meteringRepo:           gormMetering.NewMeteringRepo(adaptor.GetGORM()),
		storage:                adaptor.GetStorage(),
		security:               securityCfg,
		segmentDurationSeconds: videoCfg.GetSegmentDurationSeconds(),
		transcodeLimiter:       semaphore.NewWeighted(int64(videoCfg.GetTranscodeMaxConcurrency())),
		ffmpegRunner:           commandFFmpegRunner{},
		logger:                 logger.GetLogger().With(zap.String("module", "video_processor")),
	}
}

func (p *Processor) HandleTask(ctx context.Context, task *do.AsyncTaskDo) (string, error) {
	if task == nil {
		return "", fmt.Errorf("async task is nil")
	}
	if task.TaskType != consts.TaskTypeTranscode {
		return "", fmt.Errorf("unexpected task type: %s", task.TaskType)
	}
	if task.BizType != consts.TaskBizTypeVideoProfile {
		return "", fmt.Errorf("unexpected transcode biz type: %s", task.BizType)
	}

	if p.transcodeLimiter != nil {
		if err := p.transcodeLimiter.Acquire(ctx, 1); err != nil {
			return "", err
		}
		defer p.transcodeLimiter.Release(1)
	}

	state := &transcodeContext{taskID: task.ID}
	var retErr error
	defer func() {
		if retErr != nil {
			p.cleanupStaging(state)
			p.recordFailure(task, state, retErr)
			RecordVideoTranscode("failed", state.profileName, 0, 0)
			if p.logger != nil {
				p.logger.Warn("video transcode task failed",
					zap.Int64("task_id", state.taskID),
					zap.Int64("profile_id", state.profileID),
					zap.String("profile", state.profileName),
					zap.Int64("transcode_id", state.transcodeID),
					zap.Int64("object_id", state.objectID),
					zap.String("version_id", state.versionID),
					zap.String("ffmpeg_stderr", truncateString(retErr.Error(), maxFFmpegOutput)),
					zap.Error(retErr))
			}
		}
	}()
	profileID, err := strconv.ParseInt(task.BizID, 10, 64)
	if err != nil || profileID <= 0 {
		retErr = fmt.Errorf("invalid transcode profile id: %s", task.BizID)
		return "", retErr
	}
	state.profileID = profileID

	profile, transcode, source, err := p.loadTranscodeSource(ctx, profileID)
	if err != nil {
		retErr = err
		return "", retErr
	}
	state.profileName = profile.Profile
	state.transcodeID = transcode.ID
	state.objectID = transcode.ObjectID
	state.versionID = transcode.VersionID
	state.bucketName = transcode.BucketName
	state.finalKey = BuildProfileAssetPrefix(transcode.ID, profile.Profile)
	state.stagingKey = BuildProfileStagingAssetPrefix(transcode.ID, profile.Profile, task.ID)

	if profile.Status == consts.TranscodeStatusDone {
		return "profile already done", nil
	}
	if transcode.Status == consts.TranscodeStatusDeleted {
		retErr = fmt.Errorf("transcode is deleted")
		return "", retErr
	}
	if source.Status != consts.ObjectStatusNormal {
		retErr = fmt.Errorf("source object is not normal: status=%d", source.Status)
		return "", retErr
	}
	if source.Etag != transcode.SourceEtag {
		retErr = fmt.Errorf("source etag mismatch: got=%s want=%s", source.Etag, transcode.SourceEtag)
		return "", retErr
	}

	if err := p.markProcessing(ctx, profile.ID, transcode.ID); err != nil {
		retErr = err
		return "", retErr
	}

	tmpDir, err := os.MkdirTemp("", fmt.Sprintf("oss_video_%d_", task.ID))
	if err != nil {
		retErr = err
		return "", retErr
	}
	defer os.RemoveAll(tmpDir)

	inputPath := filepath.Join(tmpDir, "input")
	outputDir := filepath.Join(tmpDir, "out")
	if err := os.MkdirAll(outputDir, consts.FilePermDir); err != nil {
		retErr = err
		return "", retErr
	}
	if err := p.downloadSourceObject(ctx, transcode.UserID, source, inputPath); err != nil {
		retErr = err
		return "", retErr
	}

	key, err := p.getOrCreateProfileKey(ctx, transcode.ID, profile.ID)
	if err != nil {
		retErr = err
		return "", retErr
	}
	keyPath := filepath.Join(tmpDir, "enc.key")
	keyInfoPath := filepath.Join(tmpDir, "key.info")
	if err := os.WriteFile(keyPath, key.raw, consts.FilePermFile); err != nil {
		retErr = err
		return "", retErr
	}
	if err := writeKeyInfo(keyInfoPath, key.keyID, keyPath); err != nil {
		retErr = err
		return "", retErr
	}

	playlistPath := filepath.Join(outputDir, hlsPlaylistName)
	if err := p.runFFmpeg(ctx, inputPath, outputDir, keyInfoPath, profile); err != nil {
		retErr = err
		return "", retErr
	}

	stats, err := p.uploadOutputAssets(ctx, transcode.BucketName, outputDir, state.stagingKey)
	if err != nil {
		retErr = err
		return "", retErr
	}
	if err := p.storage.MoveAssetPrefix(ctx, transcode.BucketName, state.stagingKey, state.finalKey); err != nil {
		retErr = err
		return "", retErr
	}

	durationMs, err := parsePlaylistDurationMs(playlistPath)
	if err != nil {
		retErr = err
		return "", retErr
	}
	if err := p.completeProfile(ctx, transcode, profile, state.finalKey, stats, durationMs); err != nil {
		retErr = err
		return "", retErr
	}

	RecordVideoTranscode("done", profile.Profile, durationMs, stats.size)
	if p.logger != nil {
		p.logger.Info("video transcode profile completed",
			zap.Int64("task_id", state.taskID),
			zap.Int64("profile_id", state.profileID),
			zap.String("profile", profile.Profile),
			zap.Int64("transcode_id", transcode.ID),
			zap.Int64("object_id", transcode.ObjectID),
			zap.String("version_id", transcode.VersionID),
			zap.Int64("duration_ms", durationMs),
			zap.Int64("derived_size", stats.size),
			zap.Int32("segment_count", stats.segmentCount))
	}

	return fmt.Sprintf("transcode profile %s completed", profile.Profile), nil
}

func (p *Processor) loadTranscodeSource(ctx context.Context, profileID int64) (*do.VideoProfileDo, *do.VideoTranscodeDo, *do.ObjectDo, error) {
	profile, err := p.videoRepo.GetProfileByID(ctx, profileID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("get transcode profile: %w", err)
	}
	transcode, err := p.videoRepo.GetTranscodeByID(ctx, profile.TranscodeID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("get transcode: %w", err)
	}
	source, err := p.objectRepo.GetByIDAndVersion(ctx, transcode.ObjectID, transcode.VersionID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("get source object: %w", err)
	}
	return profile, transcode, source, nil
}

func (p *Processor) markProcessing(ctx context.Context, profileID int64, transcodeID int64) error {
	now := time.Now()
	processing := consts.TranscodeStatusProcessing
	emptyError := ""
	if err := p.videoRepo.UpdateProfile(ctx, profileID, &do.UpdateVideoProfile{
		Status:    &processing,
		LastError: &emptyError,
		StartedAt: &now,
	}); err != nil {
		return fmt.Errorf("mark profile processing: %w", err)
	}
	if err := p.videoRepo.UpdateTranscode(ctx, transcodeID, &do.UpdateVideoTranscode{
		Status:    &processing,
		LastError: &emptyError,
	}); err != nil {
		return fmt.Errorf("mark transcode processing: %w", err)
	}
	return nil
}

func (p *Processor) downloadSourceObject(ctx context.Context, userID int64, source *do.ObjectDo, dstPath string) error {
	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, consts.FilePermFile)
	if err != nil {
		return err
	}
	defer dst.Close()

	switch source.IsMultipart {
	case consts.ObjectIsMultipartMerged:
		return p.copyMultipartSource(ctx, userID, source, dst)
	default:
		if source.StoragePath == nil || *source.StoragePath == "" {
			return fmt.Errorf("source object storage path is empty")
		}
		return p.copyStoragePath(ctx, *source.StoragePath, dst)
	}
}

func (p *Processor) copyMultipartSource(ctx context.Context, userID int64, source *do.ObjectDo, dst io.Writer) error {
	if source.UploadID == nil || *source.UploadID == "" {
		return fmt.Errorf("multipart source missing upload_id")
	}
	parts, err := p.multipartRepo.ListMultipartParts(ctx, userID, *source.UploadID)
	if err != nil {
		return err
	}
	if len(parts) == 0 {
		return fmt.Errorf("multipart source has no parts")
	}
	sort.Slice(parts, func(i, j int) bool {
		return parts[i].PartNumber < parts[j].PartNumber
	})
	for i, part := range parts {
		expected := int32(i + 1)
		if part.PartNumber != expected {
			return fmt.Errorf("multipart part number not continuous: got=%d want=%d", part.PartNumber, expected)
		}
		if err := p.copyStoragePath(ctx, part.StoragePath, dst); err != nil {
			return fmt.Errorf("copy multipart part %d: %w", part.PartNumber, err)
		}
	}
	return nil
}

func (p *Processor) copyStoragePath(ctx context.Context, storagePath string, dst io.Writer) error {
	src, err := p.storage.Get(ctx, storagePath)
	if err != nil {
		return err
	}
	defer src.Close()

	_, err = io.Copy(dst, src)
	return err
}

func (p *Processor) getOrCreateProfileKey(ctx context.Context, transcodeID int64, profileID int64) (*profileKey, error) {
	existing, err := p.videoRepo.GetEncryptKeyByProfileID(ctx, profileID)
	if err == nil {
		return p.decryptProfileKey(existing)
	}
	if !errors.Is(err, repoerr.ErrNotFound) {
		return nil, fmt.Errorf("get profile encrypt key: %w", err)
	}

	rawKey := make([]byte, aes128KeySize())
	if _, err := rand.Read(rawKey); err != nil {
		return nil, err
	}
	keyID := uuid.NewString()
	encryptedKey, err := p.encryptProfileKey(rawKey)
	if err != nil {
		return nil, err
	}
	if err := p.videoRepo.SaveEncryptKey(ctx, &do.CreateVideoEncryptKey{
		TranscodeID:  transcodeID,
		ProfileID:    profileID,
		KeyID:        keyID,
		EncryptedKey: encryptedKey,
		Algorithm:    consts.HLSEncryptionAlgorithm,
	}); err != nil {
		existing, getErr := p.videoRepo.GetEncryptKeyByProfileID(ctx, profileID)
		if getErr != nil {
			return nil, fmt.Errorf("save profile encrypt key: %w", err)
		}
		return p.decryptProfileKey(existing)
	}
	return &profileKey{keyID: keyID, raw: rawKey}, nil
}

func (p *Processor) encryptProfileKey(rawKey []byte) ([]byte, error) {
	if len(rawKey) != aes128KeySize() {
		return nil, fmt.Errorf("invalid HLS key length: %d", len(rawKey))
	}
	masterKey, err := normalizeMasterKey(p.security.AESKey)
	if err != nil {
		return nil, err
	}
	encrypted, err := tools.AESEncrypt(string(rawKey), masterKey)
	if err != nil {
		return nil, err
	}
	return []byte(encrypted), nil
}

func (p *Processor) decryptProfileKey(key *do.VideoEncryptKeyDo) (*profileKey, error) {
	if key == nil {
		return nil, fmt.Errorf("profile encrypt key is nil")
	}
	masterKey, err := normalizeMasterKey(p.security.AESKey)
	if err != nil {
		return nil, err
	}
	raw, err := tools.AESDecrypt(string(key.EncryptedKey), masterKey)
	if err != nil {
		return nil, err
	}
	if len(raw) != aes128KeySize() {
		return nil, fmt.Errorf("invalid decrypted HLS key length: %d", len(raw))
	}
	return &profileKey{keyID: key.KeyID, raw: raw}, nil
}

func (p *Processor) runFFmpeg(ctx context.Context, inputPath string, outputDir string, keyInfoPath string, profile *do.VideoProfileDo) error {
	args := buildFFmpegArgs(inputPath, outputDir, keyInfoPath, profile, p.segmentDurationSeconds)
	output, err := p.ffmpegRunner.Run(ctx, args)
	if err != nil {
		if output == "" {
			return fmt.Errorf("ffmpeg failed: %w", err)
		}
		return fmt.Errorf("ffmpeg failed: %w: %s", err, output)
	}
	return nil
}

func (commandFFmpegRunner) Run(ctx context.Context, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	out := &tailBuffer{limit: maxFFmpegOutput}
	cmd.Stdout = out
	cmd.Stderr = out
	err := cmd.Run()
	return out.String(), err
}

func buildFFmpegArgs(inputPath string, outputDir string, keyInfoPath string, profile *do.VideoProfileDo, segmentSeconds int) []string {
	if segmentSeconds <= 0 {
		segmentSeconds = consts.HLSSegmentDurationSeconds
	}
	height := profile.Height
	if height <= 0 {
		height = 720
	}
	videoBitrate := profile.VideoBitrate
	if videoBitrate == "" {
		videoBitrate = "2000k"
	}
	audioBitrate := profile.AudioBitrate
	if audioBitrate == "" {
		audioBitrate = "128k"
	}
	return []string{
		"-y",
		"-i", inputPath,
		"-hls_key_info_file", keyInfoPath,
		"-hls_time", strconv.Itoa(segmentSeconds),
		"-hls_playlist_type", "vod",
		"-hls_segment_filename", filepath.Join(outputDir, hlsSegmentGlob),
		"-vf", fmt.Sprintf("scale=-2:%d", height),
		"-b:v", videoBitrate,
		"-b:a", audioBitrate,
		filepath.Join(outputDir, hlsPlaylistName),
	}
}

func (p *Processor) uploadOutputAssets(ctx context.Context, bucketName string, outputDir string, stagingPrefix string) (*uploadStats, error) {
	stats := &uploadStats{}
	err := filepath.WalkDir(outputDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(outputDir, path)
		if err != nil {
			return err
		}
		assetKey := stagingPrefix + "/" + filepath.ToSlash(rel)
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		result, putErr := p.storage.PutAsset(ctx, bucketName, assetKey, file)
		closeErr := file.Close()
		if putErr != nil {
			return putErr
		}
		if closeErr != nil {
			return closeErr
		}
		stats.size += result.Size
		if strings.EqualFold(filepath.Ext(rel), ".ts") {
			stats.segmentCount++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if stats.size <= 0 {
		return nil, fmt.Errorf("ffmpeg output is empty")
	}
	return stats, nil
}

func (p *Processor) completeProfile(ctx context.Context, transcode *do.VideoTranscodeDo, profile *do.VideoProfileDo, finalPrefix string, stats *uploadStats, durationMs int64) error {
	if stats == nil {
		return fmt.Errorf("upload stats is nil")
	}
	now := time.Now()
	done := consts.TranscodeStatusDone
	playlistKey := finalPrefix + "/" + hlsPlaylistName

	return p.txManager.RunInTx(ctx, func(ctx context.Context, tx tx.Tx) error {
		videoTx := p.videoRepo.WithTx(tx)
		if err := videoTx.UpdateProfile(ctx, profile.ID, &do.UpdateVideoProfile{
			Status:       &done,
			AssetPrefix:  &finalPrefix,
			PlaylistKey:  &playlistKey,
			Size:         &stats.size,
			SegmentCount: &stats.segmentCount,
			DurationMs:   &durationMs,
			FinishedAt:   &now,
		}); err != nil {
			return err
		}

		profiles, err := videoTx.ListProfiles(ctx, transcode.ID)
		if err != nil {
			return err
		}
		doneCount, derivedSize, allDone := aggregateProfiles(profiles)
		status := consts.TranscodeStatusProcessing
		var finishedAt *time.Time
		if allDone {
			status = consts.TranscodeStatusDone
			finishedAt = &now
		}
		if err := videoTx.UpdateTranscode(ctx, transcode.ID, &do.UpdateVideoTranscode{
			Status:           &status,
			DerivedSize:      &derivedSize,
			DoneProfileCount: &doneCount,
			FinishedAt:       finishedAt,
		}); err != nil {
			return err
		}

		if stats.size > 0 {
			if err := p.userRepo.WithTx(tx).UpdateStorageUsed(ctx, transcode.UserID, stats.size); err != nil {
				return err
			}
			if err := p.bucketRepo.WithTx(tx).UpdateBucketStats(ctx, transcode.UserID, transcode.BucketName, 0, stats.size); err != nil {
				return err
			}
			if err := p.meteringRepo.WithTx(tx).UpdateDailyMetrics(ctx, transcode.UserID, &transcode.BucketID, now, stats.size, 0, 0, 0, 0, 0, 0); err != nil {
				return err
			}
		}
		return nil
	})
}

func (p *Processor) cleanupStaging(state *transcodeContext) {
	if state == nil || state.bucketName == "" || state.stagingKey == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.storage.DeleteAssetPrefix(ctx, state.bucketName, state.stagingKey); err != nil {
		p.logger.Warn("failed to cleanup video staging assets",
			zap.String("bucket_name", state.bucketName),
			zap.String("staging_key", state.stagingKey),
			zap.Error(err))
	}
}

func (p *Processor) recordFailure(task *do.AsyncTaskDo, state *transcodeContext, taskErr error) {
	if state == nil || state.profileID <= 0 || taskErr == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	msg := truncateString(taskErr.Error(), maxFFmpegOutput)
	failed := consts.TranscodeStatusFailed
	now := time.Now()
	if err := p.videoRepo.UpdateProfile(ctx, state.profileID, &do.UpdateVideoProfile{
		Status:     &failed,
		LastError:  &msg,
		FinishedAt: &now,
	}); err != nil {
		p.logger.Warn("failed to mark video profile failed",
			zap.Int64("task_id", state.taskID),
			zap.Int64("profile_id", state.profileID),
			zap.Error(err))
	}

	if state.transcodeID > 0 && finalTaskAttempt(task) {
		if err := p.videoRepo.UpdateTranscode(ctx, state.transcodeID, &do.UpdateVideoTranscode{
			Status:     &failed,
			LastError:  &msg,
			FinishedAt: &now,
		}); err != nil {
			p.logger.Warn("failed to mark video transcode failed",
				zap.Int64("task_id", state.taskID),
				zap.Int64("transcode_id", state.transcodeID),
				zap.Error(err))
		}
	}
}

func BuildProfileAssetPrefix(transcodeID int64, profile string) string {
	return fmt.Sprintf("%s/%d/%s", consts.HLSAssetPrefix, transcodeID, profile)
}

func BuildProfileStagingAssetPrefix(transcodeID int64, profile string, taskID int64) string {
	return fmt.Sprintf("%s/staging/%d", BuildProfileAssetPrefix(transcodeID, profile), taskID)
}

func writeKeyInfo(path string, keyID string, keyPath string) error {
	keyURI := fmt.Sprintf("/api/v1/video/keys/%s", keyID)
	content := keyURI + "\n" + keyPath + "\n"
	return os.WriteFile(path, []byte(content), consts.FilePermFile)
}

func parsePlaylistDurationMs(path string) (int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	var totalSeconds float64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "#EXTINF:") {
			continue
		}
		value := strings.TrimPrefix(line, "#EXTINF:")
		if idx := strings.Index(value, ","); idx >= 0 {
			value = value[:idx]
		}
		seconds, parseErr := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if parseErr != nil {
			return 0, parseErr
		}
		totalSeconds += seconds
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return int64(totalSeconds * 1000), nil
}

func aggregateProfiles(profiles []*do.VideoProfileDo) (int32, int64, bool) {
	if len(profiles) == 0 {
		return 0, 0, false
	}
	doneCount := int32(0)
	derivedSize := int64(0)
	allDone := true
	for _, profile := range profiles {
		if profile == nil || profile.Status == consts.TranscodeStatusDeleted {
			continue
		}
		derivedSize += profile.Size
		if profile.Status == consts.TranscodeStatusDone {
			doneCount++
			continue
		}
		allDone = false
	}
	return doneCount, derivedSize, allDone
}

func normalizeMasterKey(raw string) ([]byte, error) {
	if isAESKeyLength(len(raw)) {
		return []byte(raw), nil
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err == nil && isAESKeyLength(len(decoded)) {
		return decoded, nil
	}
	return nil, fmt.Errorf("invalid video master key length")
}

func isAESKeyLength(length int) bool {
	return length == 16 || length == 24 || length == 32
}

func aes128KeySize() int {
	return 16
}

func finalTaskAttempt(task *do.AsyncTaskDo) bool {
	if task == nil {
		return true
	}
	maxRetry := task.MaxRetry
	if maxRetry <= 0 {
		maxRetry = 1
	}
	return task.RetryCount+1 >= maxRetry
}

func truncateString(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit]
}

type tailBuffer struct {
	limit     int
	buf       []byte
	truncated bool
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		return len(p), nil
	}
	if len(p) >= b.limit {
		b.buf = append(b.buf[:0], p[len(p)-b.limit:]...)
		b.truncated = true
		return len(p), nil
	}
	if len(b.buf)+len(p) > b.limit {
		drop := len(b.buf) + len(p) - b.limit
		b.buf = append(b.buf[drop:], p...)
		b.truncated = true
		return len(p), nil
	}
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *tailBuffer) String() string {
	if b.truncated {
		return "[truncated]\n" + string(b.buf)
	}
	return string(b.buf)
}
