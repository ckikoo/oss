package gorm

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	mysqlerr "github.com/go-sql-driver/mysql"
	mysqlgorm "gorm.io/driver/mysql"
	gormpkg "gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"oss/adaptor/repo/model"
	"oss/adaptor/repo/repoerr"
	"oss/consts"
	"oss/service/do"
)

func TestBuildTranscodeUpdates(t *testing.T) {
	status := consts.TranscodeStatusDone
	duration := int64(120000)
	derivedSize := int64(1024)
	profileCount := int32(4)
	doneProfileCount := int32(4)
	lastError := ""
	finishedAt := time.Now()

	updates := buildTranscodeUpdates(&do.UpdateVideoTranscode{
		Status:           &status,
		DurationMs:       &duration,
		DerivedSize:      &derivedSize,
		ProfileCount:     &profileCount,
		DoneProfileCount: &doneProfileCount,
		LastError:        &lastError,
		FinishedAt:       &finishedAt,
	})

	assertUpdate(t, updates, "status", status)
	assertUpdate(t, updates, "duration_ms", duration)
	assertUpdate(t, updates, "derived_size", derivedSize)
	assertUpdate(t, updates, "profile_count", profileCount)
	assertUpdate(t, updates, "done_profile_count", doneProfileCount)
	assertUpdate(t, updates, "last_error", lastError)
	assertUpdate(t, updates, "finished_at", finishedAt)
	if _, ok := updates["updated_at"]; !ok {
		t.Fatalf("updates missing updated_at")
	}
}

func TestBuildTranscodeUpdatesNilOrEmpty(t *testing.T) {
	if got := buildTranscodeUpdates(nil); got != nil {
		t.Fatalf("buildTranscodeUpdates(nil) = %v, want nil", got)
	}
	updates := buildTranscodeUpdates(&do.UpdateVideoTranscode{})
	if len(updates) != 0 {
		t.Fatalf("buildTranscodeUpdates(empty) len = %d, want 0", len(updates))
	}
}

func TestBuildProfileUpdates(t *testing.T) {
	status := consts.TranscodeStatusProcessing
	videoBitrate := "2000k"
	audioBitrate := "128k"
	width := int32(1280)
	height := int32(720)
	assetPrefix := "_video/1/720p"
	playlistKey := "_video/1/720p/index.m3u8"
	size := int64(4096)
	segmentCount := int32(12)
	duration := int64(120000)
	lastError := "retry"
	startedAt := time.Now()
	finishedAt := startedAt.Add(time.Minute)

	updates := buildProfileUpdates(&do.UpdateVideoProfile{
		Status:       &status,
		VideoBitrate: &videoBitrate,
		AudioBitrate: &audioBitrate,
		Width:        &width,
		Height:       &height,
		AssetPrefix:  &assetPrefix,
		PlaylistKey:  &playlistKey,
		Size:         &size,
		SegmentCount: &segmentCount,
		DurationMs:   &duration,
		LastError:    &lastError,
		StartedAt:    &startedAt,
		FinishedAt:   &finishedAt,
	})

	assertUpdate(t, updates, "status", status)
	assertUpdate(t, updates, "video_bitrate", videoBitrate)
	assertUpdate(t, updates, "audio_bitrate", audioBitrate)
	assertUpdate(t, updates, "width", width)
	assertUpdate(t, updates, "height", height)
	assertUpdate(t, updates, "asset_prefix", assetPrefix)
	assertUpdate(t, updates, "playlist_key", playlistKey)
	assertUpdate(t, updates, "size", size)
	assertUpdate(t, updates, "segment_count", segmentCount)
	assertUpdate(t, updates, "duration_ms", duration)
	assertUpdate(t, updates, "last_error", lastError)
	assertUpdate(t, updates, "started_at", startedAt)
	assertUpdate(t, updates, "finished_at", finishedAt)
	if _, ok := updates["updated_at"]; !ok {
		t.Fatalf("updates missing updated_at")
	}
}

func TestToVideoTranscodeDo(t *testing.T) {
	lastError := "failed"
	finishedAt := time.Now()
	modelTranscode := &model.VideoTranscode{
		ID:               1,
		UserID:           2,
		BucketID:         3,
		BucketName:       "bucket",
		ObjectID:         4,
		ObjectKey:        "movie.mp4",
		ObjectKeyHash:    "hash",
		VersionID:        "v1",
		SourceEtag:       "etag",
		SourceSize:       100,
		Status:           consts.TranscodeStatusFailed,
		DurationMs:       200,
		DerivedSize:      300,
		ProfileCount:     4,
		DoneProfileCount: 2,
		LastError:        &lastError,
		FinishedAt:       &finishedAt,
	}

	got := toVideoTranscodeDo(modelTranscode)
	if got.ID != modelTranscode.ID || got.LastError == nil || *got.LastError != lastError || got.FinishedAt == nil {
		t.Fatalf("toVideoTranscodeDo() = %+v, source = %+v", got, modelTranscode)
	}
}

func TestToVideoProfileDo(t *testing.T) {
	lastError := "failed"
	modelProfile := &model.VideoTranscodeProfile{
		ID:           1,
		TranscodeID:  2,
		Profile:      consts.VideoProfile720P,
		Status:       consts.TranscodeStatusDone,
		VideoBitrate: "2000k",
		AudioBitrate: "128k",
		Width:        1280,
		Height:       720,
		AssetPrefix:  "_video/2/720p",
		PlaylistKey:  "_video/2/720p/index.m3u8",
		Size:         1024,
		SegmentCount: 10,
		DurationMs:   120000,
		LastError:    &lastError,
	}

	got := toVideoProfileDo(modelProfile)
	if got.ID != modelProfile.ID || got.Profile != modelProfile.Profile || got.LastError == nil || *got.LastError != lastError {
		t.Fatalf("toVideoProfileDo() = %+v, source = %+v", got, modelProfile)
	}
}

func TestToVideoEncryptKeyDo(t *testing.T) {
	modelKey := &model.VideoEncryptKey{
		ID:           1,
		TranscodeID:  2,
		ProfileID:    3,
		KeyID:        "key-id",
		EncryptedKey: []byte("encrypted"),
		Algorithm:    consts.HLSEncryptionAlgorithm,
		KeyVersion:   "v1",
		KmsKeyID:     "kms",
	}

	got := toVideoEncryptKeyDo(modelKey)
	if got.ID != modelKey.ID || got.KeyID != modelKey.KeyID || string(got.EncryptedKey) != string(modelKey.EncryptedKey) {
		t.Fatalf("toVideoEncryptKeyDo() = %+v, source = %+v", got, modelKey)
	}
}

func TestCreateTranscodeDuplicateReturnsExisting(t *testing.T) {
	repo, mock := newMockVideoRepo(t)
	ctx := context.Background()
	now := time.Now()

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `video_transcodes`")).
		WillReturnError(&mysqlerr.MySQLError{Number: 1062, Message: "duplicate entry"})
	mock.ExpectQuery("SELECT \\* FROM `video_transcodes` WHERE .*object_id.*\\?.*version_id.*\\?.*LIMIT 1").
		WithArgs(int64(4), "v1").
		WillReturnRows(videoTranscodeRows().
			AddRow(10, 2, 3, "bucket", 4, "movie.mp4", "hash", "v1", "etag", 100,
				consts.TranscodeStatusPending, 0, 0, 4, 0, nil, now, now, nil))

	got, err := repo.CreateTranscode(ctx, &do.CreateVideoTranscode{
		UserID:        2,
		BucketID:      3,
		BucketName:    "bucket",
		ObjectID:      4,
		ObjectKey:     "movie.mp4",
		ObjectKeyHash: "hash",
		VersionID:     "v1",
		SourceEtag:    "etag",
		SourceSize:    100,
		Status:        consts.TranscodeStatusPending,
		ProfileCount:  4,
	})
	if err != nil {
		t.Fatalf("CreateTranscode() error = %v", err)
	}
	if got.ID != 10 || got.ObjectID != 4 || got.VersionID != "v1" {
		t.Fatalf("CreateTranscode() = %+v", got)
	}
	assertSqlExpectations(t, mock)
}

func TestGetTranscodeByIDNotFound(t *testing.T) {
	repo, mock := newMockVideoRepo(t)

	mock.ExpectQuery("SELECT \\* FROM `video_transcodes` WHERE .*id.*\\?.*LIMIT 1").
		WithArgs(int64(99)).
		WillReturnRows(videoTranscodeRows())

	_, err := repo.GetTranscodeByID(context.Background(), 99)
	if !errors.Is(err, repoerr.ErrNotFound) {
		t.Fatalf("GetTranscodeByID() error = %v, want %v", err, repoerr.ErrNotFound)
	}
	assertSqlExpectations(t, mock)
}

func TestUpdateProfileNoRowsReturnsNotFound(t *testing.T) {
	repo, mock := newMockVideoRepo(t)
	status := consts.TranscodeStatusDone

	mock.ExpectExec(regexp.QuoteMeta("UPDATE `video_transcode_profiles`")).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT count(*) FROM `video_transcode_profiles` WHERE id = ?")).
		WithArgs(int64(77)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	err := repo.UpdateProfile(context.Background(), 77, &do.UpdateVideoProfile{Status: &status})
	if !errors.Is(err, repoerr.ErrNotFound) {
		t.Fatalf("UpdateProfile() error = %v, want %v", err, repoerr.ErrNotFound)
	}
	assertSqlExpectations(t, mock)
}

func TestSaveEncryptKeyDuplicateSameProfileAndKeyIsIdempotent(t *testing.T) {
	repo, mock := newMockVideoRepo(t)
	now := time.Now()

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `video_encrypt_keys`")).
		WillReturnError(&mysqlerr.MySQLError{Number: 1062, Message: "duplicate entry"})
	mock.ExpectQuery("SELECT \\* FROM `video_encrypt_keys` WHERE .*profile_id.*\\?.*LIMIT 1").
		WithArgs(int64(3)).
		WillReturnRows(videoEncryptKeyRows().
			AddRow(1, 2, 3, "key-id", []byte("encrypted"), consts.HLSEncryptionAlgorithm, "v1", "kms", now, now))

	err := repo.SaveEncryptKey(context.Background(), &do.CreateVideoEncryptKey{
		TranscodeID:  2,
		ProfileID:    3,
		KeyID:        "key-id",
		EncryptedKey: []byte("encrypted"),
		Algorithm:    consts.HLSEncryptionAlgorithm,
		KeyVersion:   "v1",
		KmsKeyID:     "kms",
	})
	if err != nil {
		t.Fatalf("SaveEncryptKey() error = %v", err)
	}
	assertSqlExpectations(t, mock)
}

func assertUpdate[T comparable](t *testing.T, updates map[string]interface{}, key string, want T) {
	t.Helper()
	got, ok := updates[key]
	if !ok {
		t.Fatalf("updates missing key %s", key)
	}
	typed, ok := got.(T)
	if !ok {
		t.Fatalf("updates[%s] type = %T, want %T", key, got, want)
	}
	if typed != want {
		t.Fatalf("updates[%s] = %v, want %v", key, typed, want)
	}
}

func newMockVideoRepo(t *testing.T) (*VideoRepo, sqlmock.Sqlmock) {
	t.Helper()

	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}

	db, err := gormpkg.Open(mysqlgorm.New(mysqlgorm.Config{
		Conn:                      sqlDB,
		SkipInitializeWithVersion: true,
	}), &gormpkg.Config{
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
		Logger:                 gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	return &VideoRepo{db: db}, mock
}

func assertSqlExpectations(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations were not met: %v", err)
	}
}

func videoTranscodeRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id",
		"user_id",
		"bucket_id",
		"bucket_name",
		"object_id",
		"object_key",
		"object_key_hash",
		"version_id",
		"source_etag",
		"source_size",
		"status",
		"duration_ms",
		"derived_size",
		"profile_count",
		"done_profile_count",
		"last_error",
		"created_at",
		"updated_at",
		"finished_at",
	})
}

func videoEncryptKeyRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id",
		"transcode_id",
		"profile_id",
		"key_id",
		"encrypted_key",
		"algorithm",
		"key_version",
		"kms_key_id",
		"created_at",
		"updated_at",
	})
}
