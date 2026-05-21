package gorm

import (
	"context"
	"errors"
	"time"

	"oss/adaptor/repo/model"
	"oss/adaptor/repo/query"
	"oss/adaptor/repo/repoerr"
	"oss/adaptor/repo/video"
	"oss/adaptor/tx"
	"oss/consts"
	"oss/service/do"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type VideoRepo struct {
	db *gorm.DB
	q  *query.Query
}

var _ video.IVideoRepo = (*VideoRepo)(nil)

func NewVideoRepo(db *gorm.DB) video.IVideoRepo {
	return &VideoRepo{db: db, q: query.Use(db)}
}

func (r *VideoRepo) WithTx(tx tx.Tx) video.IVideoRepo {
	return &VideoRepo{db: tx.(*gorm.DB), q: query.Use(tx.(*gorm.DB))}
}

func (r *VideoRepo) CreateTranscode(ctx context.Context, in *do.CreateVideoTranscode) (*do.VideoTranscodeDo, error) {
	if in == nil {
		return nil, repoerr.ErrInvalidData
	}

	now := time.Now()
	modelTranscode := &model.VideoTranscode{
		UserID:           in.UserID,
		BucketID:         in.BucketID,
		BucketName:       in.BucketName,
		ObjectID:         in.ObjectID,
		ObjectKey:        in.ObjectKey,
		ObjectKeyHash:    in.ObjectKeyHash,
		VersionID:        in.VersionID,
		SourceEtag:       in.SourceEtag,
		SourceSize:       in.SourceSize,
		Status:           in.Status,
		DurationMs:       in.DurationMs,
		DerivedSize:      in.DerivedSize,
		ProfileCount:     in.ProfileCount,
		DoneProfileCount: in.DoneProfileCount,
		LastError:        in.LastError,
		CreatedAt:        now,
		UpdatedAt:        now,
		FinishedAt:       in.FinishedAt,
	}

	err := r.db.WithContext(ctx).Create(modelTranscode).Error
	if err != nil {
		wrapped := repoerr.Wrap(err)
		if errors.Is(wrapped, repoerr.ErrDuplicate) {
			return r.GetTranscodeByObjectVersion(ctx, in.ObjectID, in.VersionID)
		}
		return nil, wrapped
	}

	return toVideoTranscodeDo(modelTranscode), nil
}

func (r *VideoRepo) GetTranscodeByObjectVersion(ctx context.Context, objectID int64, versionID string) (*do.VideoTranscodeDo, error) {
	q := r.q.VideoTranscode
	modelTranscode, err := q.WithContext(ctx).
		Where(q.ObjectID.Eq(objectID), q.VersionID.Eq(versionID)).
		First()
	if err != nil {
		return nil, repoerr.Wrap(err)
	}
	return toVideoTranscodeDo(modelTranscode), nil
}

func (r *VideoRepo) GetTranscodeByID(ctx context.Context, transcodeID int64) (*do.VideoTranscodeDo, error) {
	q := r.q.VideoTranscode
	modelTranscode, err := q.WithContext(ctx).
		Where(q.ID.Eq(transcodeID)).
		First()
	if err != nil {
		return nil, repoerr.Wrap(err)
	}
	return toVideoTranscodeDo(modelTranscode), nil
}

func (r *VideoRepo) UpdateTranscode(ctx context.Context, transcodeID int64, in *do.UpdateVideoTranscode) error {
	updates := buildTranscodeUpdates(in)
	return updateByID(ctx, r.db, &model.VideoTranscode{}, transcodeID, updates)
}

func (r *VideoRepo) MarkTranscodeDeleted(ctx context.Context, transcodeID int64) error {
	status := consts.TranscodeStatusDeleted
	now := time.Now()
	return r.UpdateTranscode(ctx, transcodeID, &do.UpdateVideoTranscode{Status: &status, FinishedAt: &now})
}

func (r *VideoRepo) MarkProfilesDeleted(ctx context.Context, transcodeID int64) error {
	if transcodeID <= 0 {
		return repoerr.ErrInvalidData
	}
	q := r.q.VideoTranscodeProfile
	now := time.Now()
	_, err := q.WithContext(ctx).Where(q.TranscodeID.Eq(transcodeID), q.Status.Neq(consts.TranscodeStatusDeleted)).Updates(map[string]interface{}{
		q.Status.ColumnName().String():     consts.TranscodeStatusDeleted,
		q.UpdatedAt.ColumnName().String():  now,
		q.FinishedAt.ColumnName().String(): now,
	})
	return repoerr.Wrap(err)
}

func (r *VideoRepo) CreateProfiles(ctx context.Context, transcodeID int64, profiles []*do.CreateVideoProfile) ([]*do.VideoProfileDo, error) {
	modelProfiles := make([]*model.VideoTranscodeProfile, 0, len(profiles))
	profileNames := make([]string, 0, len(profiles))
	seen := map[string]struct{}{}
	now := time.Now()

	for _, profile := range profiles {
		if profile == nil || profile.Profile == "" {
			continue
		}
		if _, ok := seen[profile.Profile]; ok {
			continue
		}
		seen[profile.Profile] = struct{}{}
		profileNames = append(profileNames, profile.Profile)
		modelProfiles = append(modelProfiles, &model.VideoTranscodeProfile{
			TranscodeID:  transcodeID,
			Profile:      profile.Profile,
			Status:       profile.Status,
			VideoBitrate: profile.VideoBitrate,
			AudioBitrate: profile.AudioBitrate,
			Width:        profile.Width,
			Height:       profile.Height,
			Fps:          profile.Fps,
			AssetPrefix:  profile.AssetPrefix,
			PlaylistKey:  profile.PlaylistKey,
			Size:         profile.Size,
			SegmentCount: profile.SegmentCount,
			DurationMs:   profile.DurationMs,
			LastError:    profile.LastError,
			StartedAt:    profile.StartedAt,
			FinishedAt:   profile.FinishedAt,
			CreatedAt:    now,
			UpdatedAt:    now,
		})
	}
	if len(modelProfiles) == 0 {
		return nil, repoerr.ErrInvalidData
	}

	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "transcode_id"}, {Name: "profile"}},
		DoNothing: true,
	}).Create(&modelProfiles).Error
	if err != nil {
		return nil, repoerr.Wrap(err)
	}

	q := r.q.VideoTranscodeProfile
	modelResult, err := q.WithContext(ctx).
		Where(q.TranscodeID.Eq(transcodeID), q.Profile.In(profileNames...)).
		Order(q.Height.Desc(), q.ID.Asc()).
		Find()
	if err != nil {
		return nil, repoerr.Wrap(err)
	}
	return toVideoProfileDos(modelResult), nil
}

func (r *VideoRepo) GetProfileByID(ctx context.Context, profileID int64) (*do.VideoProfileDo, error) {
	q := r.q.VideoTranscodeProfile
	modelProfile, err := q.WithContext(ctx).Where(q.ID.Eq(profileID)).First()
	if err != nil {
		return nil, repoerr.Wrap(err)
	}
	return toVideoProfileDo(modelProfile), nil
}

func (r *VideoRepo) ListProfiles(ctx context.Context, transcodeID int64) ([]*do.VideoProfileDo, error) {
	q := r.q.VideoTranscodeProfile
	modelProfiles, err := q.WithContext(ctx).
		Where(q.TranscodeID.Eq(transcodeID)).
		Order(q.Height.Desc(), q.ID.Asc()).
		Find()
	if err != nil {
		return nil, repoerr.Wrap(err)
	}
	return toVideoProfileDos(modelProfiles), nil
}

func (r *VideoRepo) ListDoneProfiles(ctx context.Context, transcodeID int64) ([]*do.VideoProfileDo, error) {
	q := r.q.VideoTranscodeProfile
	modelProfiles, err := q.WithContext(ctx).
		Where(q.TranscodeID.Eq(transcodeID), q.Status.Eq(consts.TranscodeStatusDone)).
		Order(q.Height.Desc(), q.ID.Asc()).
		Find()
	if err != nil {
		return nil, repoerr.Wrap(err)
	}
	return toVideoProfileDos(modelProfiles), nil
}

func (r *VideoRepo) UpdateProfile(ctx context.Context, profileID int64, in *do.UpdateVideoProfile) error {
	updates := buildProfileUpdates(in)
	return updateByID(ctx, r.db, &model.VideoTranscodeProfile{}, profileID, updates)
}

func (r *VideoRepo) SaveEncryptKey(ctx context.Context, in *do.CreateVideoEncryptKey) error {
	if in == nil || in.KeyID == "" || in.ProfileID <= 0 || in.TranscodeID <= 0 || len(in.EncryptedKey) == 0 {
		return repoerr.ErrInvalidData
	}

	algorithm := in.Algorithm
	if algorithm == "" {
		algorithm = consts.HLSEncryptionAlgorithm
	}
	now := time.Now()
	modelKey := &model.VideoEncryptKey{
		TranscodeID:  in.TranscodeID,
		ProfileID:    in.ProfileID,
		KeyID:        in.KeyID,
		EncryptedKey: in.EncryptedKey,
		Algorithm:    algorithm,
		KeyVersion:   in.KeyVersion,
		KmsKeyID:     in.KmsKeyID,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	err := r.db.WithContext(ctx).Create(modelKey).Error
	if err == nil {
		return nil
	}

	wrapped := repoerr.Wrap(err)
	if !errors.Is(wrapped, repoerr.ErrDuplicate) {
		return wrapped
	}

	existing, getErr := r.GetEncryptKeyByProfileID(ctx, in.ProfileID)
	if getErr != nil {
		return wrapped
	}
	if existing.KeyID == in.KeyID {
		return nil
	}
	return wrapped
}

func (r *VideoRepo) GetEncryptKeyByKeyID(ctx context.Context, keyID string) (*do.VideoEncryptKeyDo, error) {
	q := r.q.VideoEncryptKey
	modelKey, err := q.WithContext(ctx).Where(q.KeyID.Eq(keyID)).First()
	if err != nil {
		return nil, repoerr.Wrap(err)
	}
	return toVideoEncryptKeyDo(modelKey), nil
}

func (r *VideoRepo) GetEncryptKeyByProfileID(ctx context.Context, profileID int64) (*do.VideoEncryptKeyDo, error) {
	q := r.q.VideoEncryptKey
	modelKey, err := q.WithContext(ctx).Where(q.ProfileID.Eq(profileID)).First()
	if err != nil {
		return nil, repoerr.Wrap(err)
	}
	return toVideoEncryptKeyDo(modelKey), nil
}

func buildTranscodeUpdates(in *do.UpdateVideoTranscode) map[string]interface{} {
	if in == nil {
		return nil
	}

	updates := map[string]interface{}{}
	if in.Status != nil {
		updates["status"] = *in.Status
	}
	if in.DurationMs != nil {
		updates["duration_ms"] = *in.DurationMs
	}
	if in.DerivedSize != nil {
		updates["derived_size"] = *in.DerivedSize
	}
	if in.ProfileCount != nil {
		updates["profile_count"] = *in.ProfileCount
	}
	if in.DoneProfileCount != nil {
		updates["done_profile_count"] = *in.DoneProfileCount
	}
	if in.LastError != nil {
		updates["last_error"] = *in.LastError
	}
	if in.FinishedAt != nil {
		updates["finished_at"] = *in.FinishedAt
	}
	addUpdatedAt(updates)
	return updates
}

func buildProfileUpdates(in *do.UpdateVideoProfile) map[string]interface{} {
	if in == nil {
		return nil
	}

	updates := map[string]interface{}{}
	if in.Status != nil {
		updates["status"] = *in.Status
	}
	if in.VideoBitrate != nil {
		updates["video_bitrate"] = *in.VideoBitrate
	}
	if in.AudioBitrate != nil {
		updates["audio_bitrate"] = *in.AudioBitrate
	}
	if in.Width != nil {
		updates["width"] = *in.Width
	}
	if in.Height != nil {
		updates["height"] = *in.Height
	}
	if in.AssetPrefix != nil {
		updates["asset_prefix"] = *in.AssetPrefix
	}
	if in.PlaylistKey != nil {
		updates["playlist_key"] = *in.PlaylistKey
	}
	if in.Size != nil {
		updates["size"] = *in.Size
	}
	if in.SegmentCount != nil {
		updates["segment_count"] = *in.SegmentCount
	}
	if in.DurationMs != nil {
		updates["duration_ms"] = *in.DurationMs
	}
	if in.LastError != nil {
		updates["last_error"] = *in.LastError
	}
	if in.StartedAt != nil {
		updates["started_at"] = *in.StartedAt
	}
	if in.FinishedAt != nil {
		updates["finished_at"] = *in.FinishedAt
	}
	addUpdatedAt(updates)
	return updates
}

func addUpdatedAt(updates map[string]interface{}) {
	if len(updates) > 0 {
		updates["updated_at"] = time.Now()
	}
}

func updateByID(ctx context.Context, db *gorm.DB, modelValue interface{}, id int64, updates map[string]interface{}) error {
	if id <= 0 || len(updates) == 0 {
		return repoerr.ErrInvalidData
	}

	result := db.WithContext(ctx).Model(modelValue).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return repoerr.Wrap(result.Error)
	}
	if result.RowsAffected == 0 {
		var count int64
		if err := db.WithContext(ctx).Model(modelValue).Where("id = ?", id).Count(&count).Error; err != nil {
			return repoerr.Wrap(err)
		}
		if count == 0 {
			return repoerr.ErrNotFound
		}
	}
	return nil
}

func toVideoTranscodeDo(modelTranscode *model.VideoTranscode) *do.VideoTranscodeDo {
	if modelTranscode == nil {
		return nil
	}
	return &do.VideoTranscodeDo{
		ID:               modelTranscode.ID,
		UserID:           modelTranscode.UserID,
		BucketID:         modelTranscode.BucketID,
		BucketName:       modelTranscode.BucketName,
		ObjectID:         modelTranscode.ObjectID,
		ObjectKey:        modelTranscode.ObjectKey,
		ObjectKeyHash:    modelTranscode.ObjectKeyHash,
		VersionID:        modelTranscode.VersionID,
		SourceEtag:       modelTranscode.SourceEtag,
		SourceSize:       modelTranscode.SourceSize,
		Status:           modelTranscode.Status,
		DurationMs:       modelTranscode.DurationMs,
		DerivedSize:      modelTranscode.DerivedSize,
		ProfileCount:     modelTranscode.ProfileCount,
		DoneProfileCount: modelTranscode.DoneProfileCount,
		LastError:        modelTranscode.LastError,
		CreatedAt:        modelTranscode.CreatedAt,
		UpdatedAt:        modelTranscode.UpdatedAt,
		FinishedAt:       modelTranscode.FinishedAt,
	}
}

func toVideoProfileDos(modelProfiles []*model.VideoTranscodeProfile) []*do.VideoProfileDo {
	profiles := make([]*do.VideoProfileDo, 0, len(modelProfiles))
	for _, modelProfile := range modelProfiles {
		profiles = append(profiles, toVideoProfileDo(modelProfile))
	}
	return profiles
}

func toVideoProfileDo(modelProfile *model.VideoTranscodeProfile) *do.VideoProfileDo {
	if modelProfile == nil {
		return nil
	}
	return &do.VideoProfileDo{
		ID:           modelProfile.ID,
		TranscodeID:  modelProfile.TranscodeID,
		Profile:      modelProfile.Profile,
		Status:       modelProfile.Status,
		VideoBitrate: modelProfile.VideoBitrate,
		AudioBitrate: modelProfile.AudioBitrate,
		Width:        modelProfile.Width,
		Fps:          modelProfile.Fps,
		Height:       modelProfile.Height,
		AssetPrefix:  modelProfile.AssetPrefix,
		PlaylistKey:  modelProfile.PlaylistKey,
		Size:         modelProfile.Size,
		SegmentCount: modelProfile.SegmentCount,
		DurationMs:   modelProfile.DurationMs,
		LastError:    modelProfile.LastError,
		StartedAt:    modelProfile.StartedAt,
		FinishedAt:   modelProfile.FinishedAt,
		CreatedAt:    modelProfile.CreatedAt,
		UpdatedAt:    modelProfile.UpdatedAt,
	}
}

func toVideoEncryptKeyDo(modelKey *model.VideoEncryptKey) *do.VideoEncryptKeyDo {
	if modelKey == nil {
		return nil
	}
	return &do.VideoEncryptKeyDo{
		ID:           modelKey.ID,
		TranscodeID:  modelKey.TranscodeID,
		ProfileID:    modelKey.ProfileID,
		KeyID:        modelKey.KeyID,
		EncryptedKey: modelKey.EncryptedKey,
		Algorithm:    modelKey.Algorithm,
		KeyVersion:   modelKey.KeyVersion,
		KmsKeyID:     modelKey.KmsKeyID,
		CreatedAt:    modelKey.CreatedAt,
		UpdatedAt:    modelKey.UpdatedAt,
	}
}

func (r *VideoRepo) DeleteEncryptKeysByTranscodeID(ctx context.Context, transcodeID int64) error {
	if transcodeID <= 0 {
		return repoerr.ErrInvalidData
	}
	q := r.q.VideoEncryptKey
	_, err := q.WithContext(ctx).Where(q.TranscodeID.Eq(transcodeID)).Delete()
	return repoerr.Wrap(err)
}
