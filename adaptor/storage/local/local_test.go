package local

// import (
// 	"context"
// 	"strings"
// 	"testing"
// )

// // func TestVideoAssetStorage(t *testing.T) {
// // 	ctx := context.Background()
// // 	st := New(t.TempDir())

// // 	result, err := st.PutAsset(ctx, "bucket-a", "_video/1/720p/index.m3u8", strings.NewReader("#EXTM3U"))
// // 	if err != nil {
// // 		t.Fatalf("PutAsset() error = %v", err)
// // 	}
// // 	if result.Size != int64(len("#EXTM3U")) {
// // 		t.Fatalf("PutAsset() size = %d, want %d", result.Size, len("#EXTM3U"))
// // 	}

// // 	rc, err := st.GetAsset(ctx, "bucket-a", "_video/1/720p/index.m3u8")
// // 	if err != nil {
// // 		t.Fatalf("GetAsset() error = %v", err)
// // 	}

// // 	body, err := io.ReadAll(rc)
// // 	if err != nil {
// // 		rc.Close()
// // 		t.Fatalf("ReadAll() error = %v", err)
// // 	}
// // 	if err := rc.Close(); err != nil {
// // 		t.Fatalf("Close() error = %v", err)
// // 	}
// // 	if string(body) != "#EXTM3U" {
// // 		t.Fatalf("GetAsset() body = %q, want %q", string(body), "#EXTM3U")
// // 	}

// // 	if err := st.DeleteAsset(ctx, "bucket-a", "_video/1/720p/index.m3u8"); err != nil {
// // 		t.Fatalf("DeleteAsset() error = %v", err)
// // 	}
// // 	if _, err := st.GetAsset(ctx, "bucket-a", "_video/1/720p/index.m3u8"); err == nil {
// // 		t.Fatalf("GetAsset() after DeleteAsset() error = nil, want error")
// // 	}
// // }

// func TestDeleteAssetPrefix(t *testing.T) {
// 	ctx := context.Background()
// 	st := New(t.TempDir())

// 	assets := []string{
// 		"_video/1/720p/index.m3u8",
// 		"_video/1/720p/seg_000001.ts",
// 		"_video/2/720p/index.m3u8",
// 	}
// 	for _, asset := range assets {
// 		if _, err := st.PutAsset(ctx, "bucket-a", asset, strings.NewReader(asset)); err != nil {
// 			t.Fatalf("PutAsset(%q) error = %v", asset, err)
// 		}
// 	}

// 	if err := st.DeleteAssetPrefix(ctx, "bucket-a", "_video/1"); err != nil {
// 		t.Fatalf("DeleteAssetPrefix() error = %v", err)
// 	}
// 	if _, err := st.GetAsset(ctx, "bucket-a", "_video/1/720p/index.m3u8"); err == nil {
// 		t.Fatalf("GetAsset() deleted prefix error = nil, want error")
// 	}
// 	rc, err := st.GetAsset(ctx, "bucket-a", "_video/2/720p/index.m3u8")
// 	if err != nil {
// 		t.Fatalf("GetAsset() outside deleted prefix error = %v", err)
// 	}
// 	if err := rc.Close(); err != nil {
// 		t.Fatalf("Close() outside deleted prefix error = %v", err)
// 	}
// }

// func TestMoveAssetPrefix(t *testing.T) {
// 	ctx := context.Background()
// 	st := New(t.TempDir())

// 	stagingAssets := []string{
// 		"_video/1/720p/staging/task-1/index.m3u8",
// 		"_video/1/720p/staging/task-1/seg_000001.ts",
// 	}
// 	for _, asset := range stagingAssets {
// 		if _, err := st.PutAsset(ctx, "bucket-a", asset, strings.NewReader(asset)); err != nil {
// 			t.Fatalf("PutAsset(%q) error = %v", asset, err)
// 		}
// 	}

// 	if err := st.MoveAssetPrefix(ctx, "bucket-a", "_video/1/720p/staging/task-1", "_video/1/720p"); err != nil {
// 		t.Fatalf("MoveAssetPrefix() error = %v", err)
// 	}
// 	if _, err := st.GetAsset(ctx, "bucket-a", "_video/1/720p/staging/task-1/index.m3u8"); err == nil {
// 		t.Fatalf("GetAsset() moved source error = nil, want error")
// 	}
// 	rc, err := st.GetAsset(ctx, "bucket-a", "_video/1/720p/index.m3u8")
// 	if err != nil {
// 		t.Fatalf("GetAsset() moved destination error = %v", err)
// 	}
// 	if err := rc.Close(); err != nil {
// 		t.Fatalf("Close() moved destination error = %v", err)
// 	}
// }

// func TestVideoAssetStorageRejectsUnsafeKeys(t *testing.T) {
// 	ctx := context.Background()
// 	st := New(t.TempDir())

// 	unsafeKeys := []string{
// 		"../escape.ts",
// 		"_video/../escape.ts",
// 		"/absolute.ts",
// 		"",
// 	}

// 	for _, key := range unsafeKeys {
// 		t.Run(key, func(t *testing.T) {
// 			if _, err := st.PutAsset(ctx, "bucket-a", key, strings.NewReader("x")); err == nil {
// 				t.Fatalf("PutAsset(%q) error = nil, want error", key)
// 			}
// 		})
// 	}
// }

// func TestMoveAssetPrefixRejectsDestinationInsideSource(t *testing.T) {
// 	ctx := context.Background()
// 	st := New(t.TempDir())

// 	if _, err := st.PutAsset(ctx, "bucket-a", "_video/1/index.m3u8", strings.NewReader("x")); err != nil {
// 		t.Fatalf("PutAsset() error = %v", err)
// 	}
// 	if err := st.MoveAssetPrefix(ctx, "bucket-a", "_video/1", "_video/1/nested"); err == nil {
// 		t.Fatalf("MoveAssetPrefix() error = nil, want error")
// 	}
// }

// func TestVideoAssetStorageRejectsUnsafeBuckets(t *testing.T) {
// 	ctx := context.Background()
// 	st := New(t.TempDir())

// 	unsafeBuckets := []string{"", "..", "nested/bucket", `nested\bucket`}
// 	for _, bucket := range unsafeBuckets {
// 		t.Run(bucket, func(t *testing.T) {
// 			if _, err := st.PutAsset(ctx, bucket, "_video/1/index.m3u8", strings.NewReader("x")); err == nil {
// 				t.Fatalf("PutAsset(bucket=%q) error = nil, want error", bucket)
// 			}
// 		})
// 	}
// }
