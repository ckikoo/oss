package gorm

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"

	"oss/adaptor"
	"oss/adaptor/repo/model"
	"oss/adaptor/repo/query"
	"oss/adaptor/repo/repocache"
	"oss/adaptor/repo/repoerr"
	"oss/adaptor/repo/video"
	"oss/adaptor/tx"
	"oss/consts"
	"oss/service/do"
	"oss/utils/cache"
	"oss/utils/json"
	"oss/utils/logger"

	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type VideoRepo struct {
	db           *gorm.DB
	q            *query.Query
	rds          *redis.Client
	cacheManager cache.IManager
	g            *singleflight.Group
	cacheEnabled bool
	cacheConfig  CacheInvalidationConfig
}

type CacheInvalidationConfig struct {
	DelayedDoubleDeleteEnabled bool
	DelayedDoubleDeleteDelay   time.Duration
	InvalidateBatchSize        int
}

var _ video.IVideoRepo = (*VideoRepo)(nil)

func NewVideoRepo(a adaptor.IAdaptor) video.IVideoRepo {
	db := a.GetGORM()
	return &VideoRepo{
		db:           db,
		q:            query.Use(db),
		rds:          a.GetRedis(),
		cacheManager: a.GetCache(),
		g:            &singleflight.Group{},
		cacheEnabled: true,
		cacheConfig:  defaultCacheInvalidationConfig(),
	}
}

func (r *VideoRepo) WithTx(tx tx.Tx) video.IVideoRepo {
	db := tx.(*gorm.DB)
	return &VideoRepo{
		db:           db,
		q:            query.Use(db),
		rds:          r.rds,
		cacheManager: r.cacheManager,
		g:            r.g,
		cacheEnabled: false,
		cacheConfig:  r.normalizedCacheConfig(),
	}
}

func defaultCacheInvalidationConfig() CacheInvalidationConfig {
	return CacheInvalidationConfig{
		DelayedDoubleDeleteEnabled: true,
		DelayedDoubleDeleteDelay:   200 * time.Millisecond,
		InvalidateBatchSize:        500,
	}
}

func (r *VideoRepo) normalizedCacheConfig() CacheInvalidationConfig {
	cfg := r.cacheConfig
	if cfg.DelayedDoubleDeleteDelay <= 0 {
		cfg.DelayedDoubleDeleteDelay = 200 * time.Millisecond
	}
	if cfg.InvalidateBatchSize <= 0 {
		cfg.InvalidateBatchSize = 500
	}
	return cfg
}

func (r *VideoRepo) canUseCache() bool {
	return r.cacheEnabled && r.rds != nil && r.cacheManager != nil
}

func (r *VideoRepo) getCachedTranscode(ctx context.Context, cacheKey string, queryFn func() (*do.VideoTranscodeDo, error)) (*do.VideoTranscodeDo, error) {
	if !r.canUseCache() {
		return queryFn()
	}
	if transcode, ok := getLocalCacheValue[*do.VideoTranscodeDo](r, cacheKey); ok {
		return transcode, nil
	}
	if transcode := r.getRedisTranscode(ctx, cacheKey); transcode != nil {
		r.setTranscodeLocalCaches(transcode)
		return transcode, nil
	}

	group := r.g
	if group == nil {
		transcode, err := queryFn()
		if err != nil {
			return nil, err
		}
		if ctx.Err() == nil {
			r.setTranscodeCaches(ctx, transcode)
		}
		return transcode, nil
	}

	result, err, _ := group.Do(cacheKey, func() (interface{}, error) {
		if transcode := r.getRedisTranscode(ctx, cacheKey); transcode != nil {
			return transcode, nil
		}
		transcode, err := queryFn()
		if err != nil {
			return nil, err
		}
		if ctx.Err() == nil {
			r.setTranscodeCaches(ctx, transcode)
		}
		return transcode, nil
	})
	if err != nil {
		return nil, err
	}
	transcode, ok := result.(*do.VideoTranscodeDo)
	if !ok {
		r.removeLocalCache(cacheKey)
		return nil, repoerr.ErrInvalidData
	}
	r.setTranscodeLocalCaches(transcode)
	return transcode, nil
}

func (r *VideoRepo) getCachedProfile(ctx context.Context, cacheKey string, queryFn func() (*do.VideoProfileDo, error)) (*do.VideoProfileDo, error) {
	if !r.canUseCache() {
		return queryFn()
	}
	if profile, ok := getLocalCacheValue[*do.VideoProfileDo](r, cacheKey); ok {
		return profile, nil
	}
	if profile := r.getRedisProfile(ctx, cacheKey); profile != nil {
		r.setLocalCache(cacheKey, profile)
		return profile, nil
	}

	return loadWithSingleflight(ctx, r, cacheKey, func() (*do.VideoProfileDo, error) {
		if profile := r.getRedisProfile(ctx, cacheKey); profile != nil {
			return profile, nil
		}
		profile, err := queryFn()
		if err != nil {
			return nil, err
		}
		if ctx.Err() == nil {
			r.setCacheValue(ctx, cacheKey, profile, consts.CacheTTLVideoProfile)
		}
		return profile, nil
	}, func(profile *do.VideoProfileDo) {
		if profile != nil {
			r.setLocalCache(cacheKey, profile)
		}
	})
}

func (r *VideoRepo) getCachedProfileList(ctx context.Context, cacheKey string, queryFn func() ([]*do.VideoProfileDo, error)) ([]*do.VideoProfileDo, error) {
	if !r.canUseCache() {
		return queryFn()
	}
	if profiles, ok := getLocalCacheValue[[]*do.VideoProfileDo](r, cacheKey); ok {
		return profiles, nil
	}
	if profiles := r.getRedisProfileList(ctx, cacheKey); len(profiles) > 0 {
		r.setLocalCache(cacheKey, profiles)
		return profiles, nil
	}

	return loadWithSingleflight(ctx, r, cacheKey, func() ([]*do.VideoProfileDo, error) {
		if profiles := r.getRedisProfileList(ctx, cacheKey); len(profiles) > 0 {
			return profiles, nil
		}
		profiles, err := queryFn()
		if err != nil {
			return nil, err
		}
		if len(profiles) > 0 && ctx.Err() == nil {
			r.setCacheValue(ctx, cacheKey, profiles, consts.CacheTTLVideoProfile)
		}
		return profiles, nil
	}, func(profiles []*do.VideoProfileDo) {
		if len(profiles) > 0 {
			r.setLocalCache(cacheKey, profiles)
		}
	})
}

func (r *VideoRepo) getCachedEncryptKey(ctx context.Context, cacheKey string, queryFn func() (*do.VideoEncryptKeyDo, error)) (*do.VideoEncryptKeyDo, error) {
	if !r.canUseCache() {
		return queryFn()
	}
	if key, ok := getLocalCacheValue[*do.VideoEncryptKeyDo](r, cacheKey); ok {
		return key, nil
	}
	if key := r.getRedisEncryptKey(ctx, cacheKey); key != nil {
		r.setEncryptKeyLocalCaches(key)
		return key, nil
	}

	return loadWithSingleflight(ctx, r, cacheKey, func() (*do.VideoEncryptKeyDo, error) {
		if key := r.getRedisEncryptKey(ctx, cacheKey); key != nil {
			return key, nil
		}
		key, err := queryFn()
		if err != nil {
			return nil, err
		}
		if ctx.Err() == nil {
			r.setEncryptKeyCaches(ctx, key)
		}
		return key, nil
	}, func(key *do.VideoEncryptKeyDo) {
		r.setEncryptKeyLocalCaches(key)
	})
}

func loadWithSingleflight[T any](ctx context.Context, r *VideoRepo, cacheKey string, loadFn func() (T, error), afterResult func(T)) (T, error) {
	var zero T
	group := r.g
	if group == nil {
		value, err := loadFn()
		if err != nil {
			return zero, err
		}
		afterResult(value)
		return value, nil
	}

	result, err, _ := group.Do(cacheKey, func() (interface{}, error) {
		return loadFn()
	})
	if err != nil {
		return zero, err
	}
	value, ok := result.(T)
	if !ok {
		r.removeLocalCache(cacheKey)
		return zero, repoerr.ErrInvalidData
	}
	afterResult(value)
	return value, nil
}

func getLocalCacheValue[T any](r *VideoRepo, key string) (T, bool) {
	var zero T
	if r.cacheManager == nil {
		return zero, false
	}
	entry, ok := r.cacheManager.Get(key)
	if !ok {
		return zero, false
	}
	value, ok := entry.Data.(T)
	if !ok {
		r.cacheManager.Remove(key)
		return zero, false
	}
	return value, true
}

func (r *VideoRepo) getRedisTranscode(ctx context.Context, key string) *do.VideoTranscodeDo {
	var transcode do.VideoTranscodeDo
	if !r.getRedisValue(ctx, key, &transcode) {
		return nil
	}
	return &transcode
}

func (r *VideoRepo) getRedisProfile(ctx context.Context, key string) *do.VideoProfileDo {
	var profile do.VideoProfileDo
	if !r.getRedisValue(ctx, key, &profile) {
		return nil
	}
	return &profile
}

func (r *VideoRepo) getRedisProfileList(ctx context.Context, key string) []*do.VideoProfileDo {
	var profiles []*do.VideoProfileDo
	if !r.getRedisValue(ctx, key, &profiles) {
		return nil
	}
	return profiles
}

func (r *VideoRepo) getRedisEncryptKey(ctx context.Context, key string) *do.VideoEncryptKeyDo {
	var keyInfo do.VideoEncryptKeyDo
	if !r.getRedisValue(ctx, key, &keyInfo) {
		return nil
	}
	return &keyInfo
}

func (r *VideoRepo) getRedisValue(ctx context.Context, key string, dst any) bool {
	if r.rds == nil {
		return false
	}
	raw, err := r.rds.Get(ctx, key).Result()
	if err != nil {
		if err != redis.Nil {
			logger.GetLogger().Warn("failed to get video redis cache",
				zap.Error(err),
				zap.String("key", key))
		}
		return false
	}
	if err := json.UnmarshalString(raw, dst); err != nil {
		logger.GetLogger().Warn("failed to unmarshal video redis cache",
			zap.Error(err),
			zap.String("key", key))
		return false
	}
	return true
}

func (r *VideoRepo) setTranscodeCaches(ctx context.Context, transcode *do.VideoTranscodeDo) {
	if transcode == nil {
		return
	}
	keys := []string{
		consts.VideoTranscodeCacheKey(transcode.ID),
		consts.VideoTranscodeByObjectVersionCacheKey(transcode.ObjectID, transcode.VersionID),
	}
	for _, key := range keys {
		r.setCacheValue(ctx, key, transcode, consts.CacheTTLVideoTranscode)
	}
}

func (r *VideoRepo) setTranscodeLocalCaches(transcode *do.VideoTranscodeDo) {
	if transcode == nil {
		return
	}
	r.setLocalCache(consts.VideoTranscodeCacheKey(transcode.ID), transcode)
	r.setLocalCache(consts.VideoTranscodeByObjectVersionCacheKey(transcode.ObjectID, transcode.VersionID), transcode)
}

func (r *VideoRepo) setEncryptKeyCaches(ctx context.Context, keyInfo *do.VideoEncryptKeyDo) {
	if keyInfo == nil {
		return
	}
	r.setCacheValue(ctx, consts.VideoEncryptKeyByKeyIDCacheKey(keyInfo.KeyID), keyInfo, consts.CacheTTLVideoEncryptKey)
	r.setCacheValue(ctx, consts.VideoEncryptKeyByProfileIDCacheKey(keyInfo.ProfileID), keyInfo, consts.CacheTTLVideoEncryptKey)
}

func (r *VideoRepo) setEncryptKeyLocalCaches(keyInfo *do.VideoEncryptKeyDo) {
	if keyInfo == nil {
		return
	}
	r.setLocalCache(consts.VideoEncryptKeyByKeyIDCacheKey(keyInfo.KeyID), keyInfo)
	r.setLocalCache(consts.VideoEncryptKeyByProfileIDCacheKey(keyInfo.ProfileID), keyInfo)
}

func (r *VideoRepo) setCacheValue(ctx context.Context, key string, value any, ttlSeconds int) {
	r.setLocalCache(key, value)
	if r.rds == nil {
		return
	}
	data, err := json.MarshalString(value)
	if err != nil {
		logger.GetLogger().Warn("failed to marshal video cache value",
			zap.Error(err),
			zap.String("key", key))
		return
	}
	if err := r.rds.Set(ctx, key, data, time.Duration(ttlSeconds)*time.Second).Err(); err != nil {
		logger.GetLogger().Warn("failed to set video redis cache",
			zap.Error(err),
			zap.String("key", key))
	}
}

func (r *VideoRepo) setLocalCache(key string, value any) {
	if r.cacheManager == nil || key == "" || value == nil {
		return
	}
	r.cacheManager.Set(key, value, 0)
}

func (r *VideoRepo) removeLocalCache(keys ...string) {
	if r.cacheManager == nil {
		return
	}
	r.cacheManager.Remove(keys...)
}

func (r *VideoRepo) invalidateTranscodeCacheAfterCommit(ctx context.Context, transcode *do.VideoTranscodeDo) {
	if transcode == nil {
		return
	}
	r.invalidateKeysAfterCommit(ctx,
		consts.VideoTranscodeCacheKey(transcode.ID),
		consts.VideoTranscodeByObjectVersionCacheKey(transcode.ObjectID, transcode.VersionID),
	)
}

func (r *VideoRepo) invalidateProfilesByTranscodeAfterCommit(ctx context.Context, transcodeID int64, profileIDs ...int64) {
	keys := []string{
		consts.VideoProfilesCacheKey(transcodeID),
		consts.VideoDoneProfilesCacheKey(transcodeID),
	}
	for _, profileID := range profileIDs {
		if profileID > 0 {
			keys = append(keys, consts.VideoProfileCacheKey(profileID))
		}
	}
	r.invalidateKeysAfterCommit(ctx, keys...)
}

func (r *VideoRepo) invalidateEncryptKeyCacheAfterCommit(ctx context.Context, refs ...*model.VideoEncryptKey) {
	keys := make([]string, 0, len(refs)*2)
	for _, ref := range refs {
		if ref == nil {
			continue
		}
		if ref.KeyID != "" {
			keys = append(keys, consts.VideoEncryptKeyByKeyIDCacheKey(ref.KeyID))
		}
		if ref.ProfileID > 0 {
			keys = append(keys, consts.VideoEncryptKeyByProfileIDCacheKey(ref.ProfileID))
		}
	}
	r.invalidateKeysAfterCommit(ctx, keys...)
}

func (r *VideoRepo) invalidateKeysAfterCommit(ctx context.Context, keys ...string) {
	keys = uniqueCacheKeys(keys)
	if len(keys) == 0 || (r.rds == nil && r.cacheManager == nil) {
		return
	}
	cfg := r.normalizedCacheConfig()
	repocache.Invalidator{
		RDS:          r.rds,
		Local:        r.cacheManager,
		BatchSize:    cfg.InvalidateBatchSize,
		DoubleDelete: cfg.DelayedDoubleDeleteEnabled,
		Delay:        cfg.DelayedDoubleDeleteDelay,
		LogName:      "video",
	}.AfterCommit(ctx, keys...)
}

func (r *VideoRepo) invalidateKeysWithDelayedDoubleDelete(ctx context.Context, keys ...string) {
	r.invalidateKeys(ctx, keys...)
	cfg := r.normalizedCacheConfig()
	if !cfg.DelayedDoubleDeleteEnabled || cfg.DelayedDoubleDeleteDelay <= 0 {
		return
	}
	timer := time.NewTimer(cfg.DelayedDoubleDeleteDelay)
	defer timer.Stop()
	select {
	case <-timer.C:
		r.invalidateKeys(ctx, keys...)
	case <-ctx.Done():
		logger.GetLogger().Warn("skip delayed video cache invalidation because context is done",
			zap.Error(ctx.Err()),
			zap.Strings("keys", keys))
	}
}

func (r *VideoRepo) invalidateKeys(ctx context.Context, keys ...string) {
	keys = uniqueCacheKeys(keys)
	if len(keys) == 0 {
		return
	}
	cfg := r.normalizedCacheConfig()
	for start := 0; start < len(keys); start += cfg.InvalidateBatchSize {
		end := start + cfg.InvalidateBatchSize
		if end > len(keys) {
			end = len(keys)
		}
		batch := keys[start:end]
		if r.rds != nil {
			if err := r.rds.Del(ctx, batch...).Err(); err != nil {
				logger.GetLogger().Warn("failed to delete video redis cache",
					zap.Error(err),
					zap.Strings("keys", batch))
			}
		}
		r.removeLocalCache(batch...)
		if r.cacheManager != nil {
			if err := r.cacheManager.Publish(ctx, batch...); err != nil {
				logger.GetLogger().Warn("failed to publish video cache invalidation",
					zap.Error(err),
					zap.Strings("keys", batch))
			}
		}
	}
}

func uniqueCacheKeys(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(keys))
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	return result
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
	cacheKey := consts.VideoTranscodeByObjectVersionCacheKey(objectID, versionID)
	return r.getCachedTranscode(ctx, cacheKey, func() (*do.VideoTranscodeDo, error) {
		return r.getTranscodeByObjectVersionDB(ctx, objectID, versionID)
	})
}

func (r *VideoRepo) getTranscodeByObjectVersionDB(ctx context.Context, objectID int64, versionID string) (*do.VideoTranscodeDo, error) {
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
	cacheKey := consts.VideoTranscodeCacheKey(transcodeID)
	return r.getCachedTranscode(ctx, cacheKey, func() (*do.VideoTranscodeDo, error) {
		return r.getTranscodeByIDDB(ctx, transcodeID)
	})
}

func (r *VideoRepo) getTranscodeByIDDB(ctx context.Context, transcodeID int64) (*do.VideoTranscodeDo, error) {
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
	if len(updates) == 0 {
		return repoerr.ErrInvalidData
	}
	transcode, err := r.getTranscodeByIDDB(ctx, transcodeID)
	if err != nil {
		return err
	}
	if err := updateByID(ctx, r.db, &model.VideoTranscode{}, transcodeID, updates); err != nil {
		return err
	}
	r.invalidateTranscodeCacheAfterCommit(ctx, transcode)
	return nil
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
	profileIDs, err := r.listProfileIDsByTranscode(ctx, transcodeID)
	if err != nil {
		return err
	}
	q := r.q.VideoTranscodeProfile
	now := time.Now()
	_, err = q.WithContext(ctx).Where(q.TranscodeID.Eq(transcodeID), q.Status.Neq(consts.TranscodeStatusDeleted)).Updates(map[string]interface{}{
		q.Status.ColumnName().String():     consts.TranscodeStatusDeleted,
		q.UpdatedAt.ColumnName().String():  now,
		q.FinishedAt.ColumnName().String(): now,
	})
	if err != nil {
		return repoerr.Wrap(err)
	}
	r.invalidateProfilesByTranscodeAfterCommit(ctx, transcodeID, profileIDs...)
	return nil
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
	result := toVideoProfileDos(modelResult)
	r.invalidateProfilesByTranscodeAfterCommit(ctx, transcodeID, profileIDsFromDos(result)...)
	return result, nil
}

func (r *VideoRepo) GetProfileByID(ctx context.Context, profileID int64) (*do.VideoProfileDo, error) {
	cacheKey := consts.VideoProfileCacheKey(profileID)
	return r.getCachedProfile(ctx, cacheKey, func() (*do.VideoProfileDo, error) {
		return r.getProfileByIDDB(ctx, profileID)
	})
}

func (r *VideoRepo) getProfileByIDDB(ctx context.Context, profileID int64) (*do.VideoProfileDo, error) {
	q := r.q.VideoTranscodeProfile
	modelProfile, err := q.WithContext(ctx).Where(q.ID.Eq(profileID)).First()
	if err != nil {
		return nil, repoerr.Wrap(err)
	}
	return toVideoProfileDo(modelProfile), nil
}

func (r *VideoRepo) ListProfiles(ctx context.Context, transcodeID int64) ([]*do.VideoProfileDo, error) {
	cacheKey := consts.VideoProfilesCacheKey(transcodeID)
	return r.getCachedProfileList(ctx, cacheKey, func() ([]*do.VideoProfileDo, error) {
		return r.listProfilesDB(ctx, transcodeID)
	})
}

func (r *VideoRepo) listProfilesDB(ctx context.Context, transcodeID int64) ([]*do.VideoProfileDo, error) {
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
	cacheKey := consts.VideoDoneProfilesCacheKey(transcodeID)
	return r.getCachedProfileList(ctx, cacheKey, func() ([]*do.VideoProfileDo, error) {
		return r.listDoneProfilesDB(ctx, transcodeID)
	})
}

func (r *VideoRepo) listDoneProfilesDB(ctx context.Context, transcodeID int64) ([]*do.VideoProfileDo, error) {
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
	if len(updates) == 0 {
		return repoerr.ErrInvalidData
	}
	profile, err := r.getProfileByIDDB(ctx, profileID)
	if err != nil {
		return err
	}
	if err := updateByID(ctx, r.db, &model.VideoTranscodeProfile{}, profileID, updates); err != nil {
		return err
	}
	r.invalidateProfilesByTranscodeAfterCommit(ctx, profile.TranscodeID, profileID)
	return nil
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
		r.invalidateEncryptKeyCacheAfterCommit(ctx, modelKey)
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
		r.invalidateKeysAfterCommit(ctx,
			consts.VideoEncryptKeyByKeyIDCacheKey(existing.KeyID),
			consts.VideoEncryptKeyByProfileIDCacheKey(existing.ProfileID),
		)
		return nil
	}
	return wrapped
}

func (r *VideoRepo) GetEncryptKeyByKeyID(ctx context.Context, keyID string) (*do.VideoEncryptKeyDo, error) {
	cacheKey := consts.VideoEncryptKeyByKeyIDCacheKey(keyID)
	return r.getCachedEncryptKey(ctx, cacheKey, func() (*do.VideoEncryptKeyDo, error) {
		return r.getEncryptKeyByKeyIDDB(ctx, keyID)
	})
}

func (r *VideoRepo) getEncryptKeyByKeyIDDB(ctx context.Context, keyID string) (*do.VideoEncryptKeyDo, error) {
	q := r.q.VideoEncryptKey
	modelKey, err := q.WithContext(ctx).Where(q.KeyID.Eq(keyID)).First()
	if err != nil {
		return nil, repoerr.Wrap(err)
	}
	return toVideoEncryptKeyDo(modelKey), nil
}

func (r *VideoRepo) GetEncryptKeyByProfileID(ctx context.Context, profileID int64) (*do.VideoEncryptKeyDo, error) {
	cacheKey := consts.VideoEncryptKeyByProfileIDCacheKey(profileID)
	return r.getCachedEncryptKey(ctx, cacheKey, func() (*do.VideoEncryptKeyDo, error) {
		return r.getEncryptKeyByProfileIDDB(ctx, profileID)
	})
}

func (r *VideoRepo) getEncryptKeyByProfileIDDB(ctx context.Context, profileID int64) (*do.VideoEncryptKeyDo, error) {
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

func (r *VideoRepo) listProfileIDsByTranscode(ctx context.Context, transcodeID int64) ([]int64, error) {
	var profileIDs []int64
	if err := r.db.WithContext(ctx).
		Model(&model.VideoTranscodeProfile{}).
		Where("transcode_id = ?", transcodeID).
		Pluck("id", &profileIDs).Error; err != nil {
		return nil, repoerr.Wrap(err)
	}
	return profileIDs, nil
}

func profileIDsFromDos(profiles []*do.VideoProfileDo) []int64 {
	ids := make([]int64, 0, len(profiles))
	for _, profile := range profiles {
		if profile != nil && profile.ID > 0 {
			ids = append(ids, profile.ID)
		}
	}
	return ids
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
	keys, err := q.WithContext(ctx).Where(q.TranscodeID.Eq(transcodeID)).Find()
	if err != nil {
		return repoerr.Wrap(err)
	}
	_, err = q.WithContext(ctx).Where(q.TranscodeID.Eq(transcodeID)).Delete()
	if err != nil {
		return repoerr.Wrap(err)
	}
	r.invalidateEncryptKeyCacheAfterCommit(ctx, keys...)
	return nil
}
