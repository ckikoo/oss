// 校验权限可以走 middleware 禁止再service 校验

package video

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"oss/adaptor"
	"oss/adaptor/redis"
	"oss/adaptor/repo/bucket"
	gormBucket "oss/adaptor/repo/bucket/gorm"
	"oss/adaptor/repo/metering"
	gormMetering "oss/adaptor/repo/metering/gorm"
	"oss/adaptor/repo/object"
	gormObject "oss/adaptor/repo/object/gorm"
	"oss/adaptor/repo/repoerr"
	videoRepo "oss/adaptor/repo/video"
	gormVideo "oss/adaptor/repo/video/gorm"
	"oss/adaptor/storage"
	"oss/common"
	"oss/config"
	"oss/consts"
	"oss/service/do"
	"oss/service/dto"
	policySvc "oss/service/policy"
	"oss/utils/logger"
	"oss/utils/tools"

	"go.uber.org/zap"
)

const (
	contentTypeMPEGURL = "application/vnd.apple.mpegurl"
	contentTypeTS      = "video/MP2T"
	contentTypeBinary  = "application/octet-stream"
)

type PlaybackService struct {
	videoRepo    videoRepo.IVideoRepo
	objectRepo   object.IObjectRepo
	bucketRepo   bucket.IBucketRepo
	meteringRepo metering.IMeteringRepo
	playToken    redis.IPlayToken
	policy       playPolicyEvaluator
	storage      storage.IStorage
	security     config.Security
	videoCfg     config.Video
	logger       *zap.Logger
}

type HLSContent struct {
	Body        io.ReadCloser
	ContentType string
}

type playPolicyEvaluator interface {
	Evaluate(ctx context.Context, req do.EvaluateReq) consts.Effect
}

type playBinding struct {
	Object       *do.ObjectDo
	Transcode    *do.VideoTranscodeDo
	DoneProfiles []*do.VideoProfileDo
}

func NewPlaybackService(adaptor adaptor.IAdaptor) *PlaybackService {
	videoCfg := config.Video{}
	securityCfg := config.Security{}
	if cfg := adaptor.GetConfig(); cfg != nil {
		videoCfg = cfg.Video
		securityCfg = cfg.Security
	}

	return &PlaybackService{
		videoRepo:    gormVideo.NewVideoRepo(adaptor),
		objectRepo:   gormObject.NewObjectRepo(adaptor),
		bucketRepo:   gormBucket.NewBucketRepo(adaptor),
		meteringRepo: gormMetering.NewMeteringRepo(adaptor.GetGORM()),
		playToken:    redis.NewPlayToken(adaptor),
		policy:       policySvc.NewService(adaptor),
		storage:      adaptor.GetStorage(),
		security:     securityCfg,
		videoCfg:     videoCfg,
		logger:       logger.GetLogger().With(zap.String("module", "video_playback")),
	}
}

func (s *PlaybackService) CreatePlayToken(ctx *common.UserInfoCtx, req *dto.CreateVideoPlayTokenReq) (*dto.CreateVideoPlayTokenResp, common.Errno) {
	metricResult := "error"
	defer func() { RecordVideoPlayToken(metricResult) }()

	req.BucketName = strings.TrimSpace(req.BucketName)
	req.ObjectKey = strings.TrimSpace(req.ObjectKey)
	req.VersionID = strings.TrimSpace(req.VersionID)
	obj, err := s.objectRepo.GetByKey(ctx, req.BucketName, req.ObjectKey, req.VersionID)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	if obj == nil {
		return nil, common.ResouceNotFoundErr
	}

	videoTranscodeDo, err := s.videoRepo.GetTranscodeByObjectVersion(ctx, obj.ID, obj.VersionID)
	if err != nil {
		if errors.Is(err, repoerr.ErrNotFound) {
			metricResult = "pending"
			return &dto.CreateVideoPlayTokenResp{
				Status:   consts.TranscodeStatusPending,
				Profiles: []string{},
			}, common.OK
		}
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	if videoTranscodeDo == nil {
		metricResult = "pending"
		return &dto.CreateVideoPlayTokenResp{
			Status:   consts.TranscodeStatusPending,
			Profiles: []string{},
		}, common.OK
	}
	if videoTranscodeDo.Status == consts.TranscodeStatusDeleted {
		return nil, common.PermissionErr.WithMsg("transcode is deleted")
	}

	profiles, err := s.videoRepo.ListDoneProfiles(ctx, videoTranscodeDo.ID)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	if len(profiles) == 0 {
		metricResult = "not_ready"
		return &dto.CreateVideoPlayTokenResp{
			Status:      videoTranscodeDo.Status,
			TranscodeID: videoTranscodeDo.ID,
			Profiles:    []string{},
		}, common.OK
	}

	expiresIn := req.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = int64(s.videoCfg.GetPlayTokenTTLSeconds())
	}
	expiresAt := time.Now().Add(time.Duration(expiresIn) * time.Second).Unix()
	// TODO 待修复 后续可以改成不适用redis， 存计算
	token := tools.UUIDHex()
	claims := s.buildPlayTokenClaims(ctx.UserID, obj, videoTranscodeDo, token, expiresAt)

	if err := s.playToken.CreatePlayToken(ctx, token, claims, time.Duration(expiresIn)*time.Second); err != nil {
		return nil, common.RedisErr.WithErr(err)
	}

	metricResult = "success"
	return &dto.CreateVideoPlayTokenResp{
		Token:       token,
		PlayURL:     fmt.Sprintf("/api/v1/video/hls/%d/master.m3u8", videoTranscodeDo.ID),
		ExpiresAt:   expiresAt,
		Status:      videoTranscodeDo.Status,
		TranscodeID: videoTranscodeDo.ID,
		Profiles:    profileNames(profiles),
	}, common.OK
}

func (s *PlaybackService) GetTranscodeStatus(ctx *common.UserInfoCtx, bucketName string, objectKey string, versionID string) (*dto.GetVideoTranscodeStatusResp, common.Errno) {
	binfo, err := s.bucketRepo.GetByUserAndName(ctx, ctx.UserID, bucketName)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	if binfo == nil {
		return nil, common.ResouceNotFoundErr.WithMsg("bucket not found")
	}

	obj, err := s.objectRepo.GetByKey(ctx, bucketName, objectKey, versionID)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	if !visibleVideoObject(obj) {
		return nil, common.ResouceNotFoundErr.WithMsg("object not found")
	}

	resp := &dto.GetVideoTranscodeStatusResp{
		ObjectKey: obj.ObjectKey,
		VersionID: obj.VersionID,
		Status:    consts.TranscodeStatusPending,
		Profiles:  []*dto.VideoProfileStatus{},
	}

	transcode, err := s.videoRepo.GetTranscodeByObjectVersion(ctx, obj.ID, obj.VersionID)
	if err != nil {
		if errors.Is(err, repoerr.ErrNotFound) {
			return resp, common.OK
		}
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}
	if transcode == nil {
		return resp, common.OK
	}

	resp.Status = transcode.Status
	resp.DurationMs = transcode.DurationMs
	resp.DerivedSize = transcode.DerivedSize
	resp.TranscodeID = transcode.ID

	profiles, err := s.videoRepo.ListProfiles(ctx, transcode.ID)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}
	resp.Profiles = buildProfileStatusItems(profiles)
	return resp, common.OK
}

func (s *PlaybackService) GetMasterPlaylist(ctx *common.VideoPlayTokenCtx, transcodeID int64) (*HLSContent, common.Errno) {
	transcode, err := s.videoRepo.GetTranscodeByID(ctx, transcodeID)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	if errno := s.validateClaimsForTranscode(ctx, transcode); errno.NotOk() {
		return nil, errno
	}

	profiles, err := s.videoRepo.ListDoneProfiles(ctx, transcode.ID)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	playlist := renderMasterPlaylist(transcode.ID, profiles)
	return stringContent(playlist, contentTypeMPEGURL), common.OK
}

func (s *PlaybackService) GetProfilePlaylist(ctx *common.VideoPlayTokenCtx, transcodeID int64, profileName string) (*HLSContent, common.Errno) {
	transcode, err := s.videoRepo.GetTranscodeByID(ctx, transcodeID)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	if errno := s.validateClaimsForTranscode(ctx, transcode); errno.NotOk() {
		return nil, errno
	}

	profile, errno := s.getDoneProfile(ctx, transcode.ID, profileName)
	if errno.NotOk() {
		return nil, errno
	}

	keyInfo, err := s.videoRepo.GetEncryptKeyByProfileID(ctx, profile.ID)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	assetKey := profile.PlaylistKey
	if assetKey == "" {
		assetKey = profile.AssetPrefix + "/" + hlsPlaylistName
	}
	rc, err := s.storage.GetAsset(ctx, transcode.BucketName, assetKey)
	if err != nil {
		return nil, common.ResouceNotFoundErr.WithErr(err)
	}
	defer rc.Close()

	body, err := io.ReadAll(rc)
	if err != nil {
		return nil, common.ServerErr.WithErr(err)
	}
	playlist := rewritePlaylistKeyURI(string(body), "/api/v1/video/keys/"+keyInfo.KeyID)
	return stringContent(playlist, contentTypeMPEGURL), common.OK
}

func (s *PlaybackService) GetSegment(ctx *common.VideoPlayTokenCtx, transcodeID int64, profileName string, segment string) (*HLSContent, common.Errno) {
	transcode, err := s.videoRepo.GetTranscodeByID(ctx, transcodeID)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	if errno := s.validateClaimsForTranscode(ctx, transcode); errno.NotOk() {
		return nil, errno
	}

	profile, errno := s.getDoneProfile(ctx, transcode.ID, profileName)
	if errno.NotOk() {
		return nil, errno
	}
	if !validSegmentName(segment) {
		return nil, common.ParamErr.WithMsg("invalid segment")
	}

	assetKey := profile.AssetPrefix + "/" + segment
	rc, err := s.storage.GetAsset(ctx, transcode.BucketName, assetKey)
	if err != nil {
		return nil, common.ResouceNotFoundErr.WithErr(err)
	}
	metered := s.meterSegmentDownload(ctx, transcode, profile.Profile, segment, rc)
	return &HLSContent{Body: metered, ContentType: segmentContentType(segment)}, common.OK
}

func (s *PlaybackService) meterSegmentDownload(ctx *common.VideoPlayTokenCtx, transcode *do.VideoTranscodeDo, profile string, segment string, rc io.ReadCloser) io.ReadCloser {
	return &meteredSegmentReadCloser{
		ReadCloser: rc,
		onClose: func(bytesRead int64) {
			if bytesRead > 0 {
				RecordVideoSegmentBytes(bytesRead)
			}
			if s.meteringRepo == nil || transcode == nil || ctx == nil {
				return
			}
			if err := s.meteringRepo.UpdateDailyMetrics(ctx, transcode.UserID, &transcode.BucketID, time.Now(), 0, 0, 0, bytesRead, 1, 0, 0); err != nil && s.logger != nil {
				s.logger.Warn("failed to meter video segment download",
					zap.Int64("transcode_id", transcode.ID),
					zap.Int64("object_id", transcode.ObjectID),
					zap.String("version_id", transcode.VersionID),
					zap.String("profile", profile),
					zap.String("segment", segment),
					zap.Int64("bytes", bytesRead),
					zap.Error(err))
			}
		},
	}
}

func (s *PlaybackService) GetKey(ctx *common.VideoPlayTokenCtx, keyID string) (*HLSContent, common.Errno) {
	metricResult := "error"
	defer func() { RecordVideoKeyRequest(metricResult) }()

	keyInfo, err := s.videoRepo.GetEncryptKeyByKeyID(ctx, keyID)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}
	if keyInfo == nil {
		return nil, common.ResouceNotFoundErr.WithMsg("video key not found")
	}

	profile, err := s.videoRepo.GetProfileByID(ctx, keyInfo.ProfileID)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}
	if profile == nil {
		return nil, common.ResouceNotFoundErr.WithMsg("video profile not found")
	}
	if profile.Status == consts.TranscodeStatusDeleted {
		return nil, common.PermissionErr.WithMsg("video profile is deleted")
	}
	if profile.TranscodeID != keyInfo.TranscodeID {
		return nil, common.PermissionErr.WithMsg("video key profile mismatch")
	}

	transcode, err := s.videoRepo.GetTranscodeByID(ctx, keyInfo.TranscodeID)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}
	if errno := s.validateClaimsForTranscode(ctx, transcode); errno.NotOk() {
		return nil, errno
	}

	rawKey, err := s.decryptHLSKey(keyInfo)
	if err != nil {
		return nil, common.ServerErr.WithErr(err)
	}
	metricResult = "success"
	return &HLSContent{Body: io.NopCloser(bytes.NewReader(rawKey)), ContentType: contentTypeBinary}, common.OK
}

type meteredSegmentReadCloser struct {
	io.ReadCloser
	bytesRead int64
	onClose   func(bytesRead int64)
}

func (r *meteredSegmentReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.bytesRead += int64(n)
	return n, err
}

func (r *meteredSegmentReadCloser) Close() error {
	err := r.ReadCloser.Close()
	if r.onClose != nil {
		r.onClose(r.bytesRead)
	}
	return err
}

func (s *PlaybackService) buildPlayTokenClaims(userID int64, obj *do.ObjectDo, transcode *do.VideoTranscodeDo, token string, expiresAt int64) *dto.VideoPlayToken {
	return &dto.VideoPlayToken{
		Token:       token,
		UserID:      userID,
		BucketID:    transcode.BucketID,
		BucketName:  transcode.BucketName,
		ObjectID:    obj.ID,
		ObjectKey:   transcode.ObjectKey,
		VersionID:   transcode.VersionID,
		TranscodeID: transcode.ID,
		ExpiresAt:   expiresAt,
		Action:      consts.PlayVideoAction,
	}
}

func (s *PlaybackService) validateClaimsForTranscode(claims *common.VideoPlayTokenCtx, transcode *do.VideoTranscodeDo) common.Errno {
	if transcode == nil {
		return common.ResouceNotFoundErr.WithMsg("transcode not found")
	}
	if claims.TranscodeID != transcode.ID {
		return common.PermissionErr.WithMsg("play token transcode mismatch")
	}
	if transcode.Status == consts.TranscodeStatusDeleted {
		return common.PermissionErr.WithMsg("transcode is deleted")
	}
	if claims.ObjectID != transcode.ObjectID || claims.VersionID != transcode.VersionID {
		return common.PermissionErr.WithMsg("play token object mismatch")
	}
	if claims.BucketID > 0 && claims.BucketID != transcode.BucketID {
		return common.PermissionErr.WithMsg("play token bucket mismatch")
	}
	return common.OK
}

func (s *PlaybackService) decryptHLSKey(keyInfo *do.VideoEncryptKeyDo) ([]byte, error) {
	if keyInfo == nil {
		return nil, fmt.Errorf("video encrypt key is nil")
	}
	masterKey, err := normalizeMasterKey(s.security.AESKey)
	if err != nil {
		return nil, err
	}
	raw, err := tools.AESDecrypt(string(keyInfo.EncryptedKey), masterKey)
	if err != nil {
		return nil, err
	}
	if len(raw) != aes128KeySize() {
		return nil, fmt.Errorf("invalid decrypted HLS key length: %d", len(raw))
	}
	return raw, nil
}

func (s *PlaybackService) getDoneProfile(ctx context.Context, transcodeID int64, profileName string) (*do.VideoProfileDo, common.Errno) {
	if strings.TrimSpace(profileName) == "" {
		return nil, common.ParamErr.WithMsg("profile is required")
	}

	profiles, err := s.videoRepo.ListDoneProfiles(ctx, transcodeID)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}
	for _, profile := range profiles {
		if profile != nil && profile.Profile == profileName {
			return profile, common.OK
		}
	}
	return nil, common.ResouceNotFoundErr.WithMsg("profile not ready")
}

func renderMasterPlaylist(transcodeID int64, profiles []*do.VideoProfileDo) string {
	profiles = append([]*do.VideoProfileDo(nil), profiles...)
	sort.SliceStable(profiles, func(i, j int) bool {
		if profiles[i] == nil {
			return false
		}
		if profiles[j] == nil {
			return true
		}
		if profiles[i].Height == profiles[j].Height {
			return profiles[i].Profile < profiles[j].Profile
		}
		return profiles[i].Height > profiles[j].Height
	})

	var sb strings.Builder
	sb.WriteString("#EXTM3U\n")
	sb.WriteString("#EXT-X-VERSION:3\n")
	for _, profile := range profiles {
		if profile == nil || profile.Status != consts.TranscodeStatusDone {
			continue
		}
		bandwidth := estimateBandwidth(profile.VideoBitrate, profile.AudioBitrate)
		sb.WriteString("#EXT-X-STREAM-INF:BANDWIDTH=")
		sb.WriteString(strconv.FormatInt(bandwidth, 10))
		if profile.Width > 0 && profile.Height > 0 {
			sb.WriteString(",RESOLUTION=")
			sb.WriteString(strconv.FormatInt(int64(profile.Width), 10))
			sb.WriteString("x")
			sb.WriteString(strconv.FormatInt(int64(profile.Height), 10))
		}
		sb.WriteString("\n")
		sb.WriteString(fmt.Sprintf("/api/v1/video/hls/%d/%s/index.m3u8\n", transcodeID, profile.Profile))
	}
	return sb.String()
}

func rewritePlaylistKeyURI(playlist string, keyURI string) string {
	lines := strings.Split(playlist, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "#EXT-X-KEY:") {
			continue
		}
		lines[i] = rewriteAttribute(line, "URI", keyURI)
	}
	return strings.Join(lines, "\n")
}

func rewriteAttribute(line string, name string, value string) string {
	prefix := name + "="
	idx := strings.Index(line, prefix)
	if idx < 0 {
		if strings.HasSuffix(line, ":") || strings.HasSuffix(line, ",") {
			return line + prefix + quote(value)
		}
		return line + "," + prefix + quote(value)
	}

	start := idx + len(prefix)
	if start < len(line) && line[start] == '"' {
		end := strings.Index(line[start+1:], `"`)
		if end < 0 {
			return line[:start] + quote(value)
		}
		end = start + 1 + end + 1
		return line[:start] + quote(value) + line[end:]
	}

	end := start
	for end < len(line) && line[end] != ',' {
		end++
	}
	return line[:start] + quote(value) + line[end:]
}

func quote(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `%22`) + `"`
}

func estimateBandwidth(videoBitrate string, audioBitrate string) int64 {
	bandwidth := parseBitrate(videoBitrate) + parseBitrate(audioBitrate)
	if bandwidth <= 0 {
		return 1_000_000
	}
	return bandwidth
}

func parseBitrate(raw string) int64 {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return 0
	}
	multiplier := int64(1)
	switch {
	case strings.HasSuffix(raw, "k"):
		multiplier = 1000
		raw = strings.TrimSuffix(raw, "k")
	case strings.HasSuffix(raw, "m"):
		multiplier = 1000 * 1000
		raw = strings.TrimSuffix(raw, "m")
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0
	}
	return value * multiplier
}

func validSegmentName(segment string) bool {
	if segment == "" || segment != path.Base(segment) {
		return false
	}
	if strings.Contains(segment, `\`) || strings.Contains(segment, "..") {
		return false
	}
	return strings.EqualFold(path.Ext(segment), ".ts")
}

func segmentContentType(segment string) string {
	if strings.EqualFold(path.Ext(segment), ".ts") {
		return contentTypeTS
	}
	if typ := mime.TypeByExtension(path.Ext(segment)); typ != "" {
		return typ
	}
	return "application/octet-stream"
}

func visibleVideoObject(obj *do.ObjectDo) bool {
	return obj != nil && obj.Status == consts.ObjectStatusNormal
}

func objectContentType(obj *do.ObjectDo) string {
	if obj == nil || obj.ContentType == nil {
		return ""
	}
	return *obj.ContentType
}

func profileNames(profiles []*do.VideoProfileDo) []string {
	names := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		if profile == nil || profile.Status != consts.TranscodeStatusDone {
			continue
		}
		names = append(names, profile.Profile)
	}
	return names
}

func buildProfileStatusItems(profiles []*do.VideoProfileDo) []*dto.VideoProfileStatus {
	items := make([]*dto.VideoProfileStatus, 0, len(profiles))
	for _, profile := range profiles {
		if profile == nil {
			continue
		}
		lastError := ""
		if profile.LastError != nil {
			lastError = *profile.LastError
		}
		items = append(items, &dto.VideoProfileStatus{
			Profile:      profile.Profile,
			Status:       profile.Status,
			Width:        profile.Width,
			Height:       profile.Height,
			Size:         profile.Size,
			SegmentCount: profile.SegmentCount,
			DurationMs:   profile.DurationMs,
			LastError:    lastError,
		})
	}
	return items
}

func stringContent(body string, contentType string) *HLSContent {
	return &HLSContent{
		Body:        io.NopCloser(strings.NewReader(body)),
		ContentType: contentType,
	}
}
