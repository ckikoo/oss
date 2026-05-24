package s3

import (
	"testing"
	"time"

	"oss/service/do"
	"oss/service/dto"
)

func TestBuildS3ListObjectsV2RespWithDelimiter(t *testing.T) {
	req := &dto.S3ListObjectsV2Req{
		BucketName: "demo",
		Prefix:     "photos/",
		Delimiter:  "/",
		MaxKeys:    10,
	}
	items := []*dto.ObjectItem{
		{ObjectKey: "photos/2026/a.jpg", Etag: "etag-a", Size: 10, StorageClass: "STANDARD"},
		{ObjectKey: "photos/2026/b.jpg", Etag: "etag-b", Size: 20, StorageClass: "STANDARD"},
		{ObjectKey: "photos/root.jpg", Etag: "etag-root", Size: 30, StorageClass: "STANDARD"},
	}

	got := buildS3ListObjectsV2Resp(req, items, req.MaxKeys, false)

	if got.KeyCount != 2 {
		t.Fatalf("KeyCount = %d, want 2", got.KeyCount)
	}
	if len(got.CommonPrefixes) != 1 || got.CommonPrefixes[0].Prefix != "photos/2026/" {
		t.Fatalf("CommonPrefixes = %+v, want photos/2026/", got.CommonPrefixes)
	}
	if len(got.Contents) != 1 || got.Contents[0].Key != "photos/root.jpg" {
		t.Fatalf("Contents = %+v, want only photos/root.jpg", got.Contents)
	}
	if got.Contents[0].ETag != `"etag-root"` {
		t.Fatalf("ETag = %q, want quoted etag", got.Contents[0].ETag)
	}
}

func TestBuildS3ListObjectsV2RespContinuationToken(t *testing.T) {
	req := &dto.S3ListObjectsV2Req{
		BucketName:        "demo",
		Prefix:            "logs/",
		ContinuationToken: "logs/0001",
		StartAfter:        "logs/0000",
	}
	items := []*dto.ObjectItem{
		{ObjectKey: "logs/0002", Etag: "etag-2", StorageClass: "STANDARD"},
		{ObjectKey: "logs/0003", Etag: "etag-3", StorageClass: "STANDARD"},
	}

	got := buildS3ListObjectsV2Resp(req, items, 2, true)

	if !got.IsTruncated {
		t.Fatal("IsTruncated = false, want true")
	}
	if got.ContinuationToken != "logs/0001" {
		t.Fatalf("ContinuationToken = %q, want logs/0001", got.ContinuationToken)
	}
	if got.NextContinuationToken != "logs/0003" {
		t.Fatalf("NextContinuationToken = %q, want logs/0003", got.NextContinuationToken)
	}
	if got.StartAfter != "logs/0000" {
		t.Fatalf("StartAfter = %q, want logs/0000", got.StartAfter)
	}
}

func TestNormalizeS3LocationConstraint(t *testing.T) {
	tests := []struct {
		name   string
		region string
		want   string
	}{
		{name: "empty", region: "", want: ""},
		{name: "us east one", region: "us-east-1", want: ""},
		{name: "other region", region: "cn-north-1", want: "cn-north-1"},
		{name: "trims spaces", region: " eu-west-1 ", want: "eu-west-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeS3LocationConstraint(tt.region); got != tt.want {
				t.Fatalf("normalizeS3LocationConstraint(%q) = %q, want %q", tt.region, got, tt.want)
			}
		})
	}
}

func TestBuildS3ListPartsRespAppliesMarkerAndMaxParts(t *testing.T) {
	createdAt := time.Date(2026, 5, 24, 1, 2, 3, 0, time.UTC)
	req := &dto.S3ListPartsReq{
		BucketName:       "demo",
		ObjectKey:        "large.bin",
		UploadID:         "upload-1",
		PartNumberMarker: 1,
		MaxParts:         2,
	}
	parts := []*do.MultipartPartDo{
		{PartNumber: 1, Etag: "etag-1", Size: 10, CreatedAt: createdAt},
		{PartNumber: 2, Etag: "etag-2", Size: 20, CreatedAt: createdAt},
		{PartNumber: 3, Etag: "etag-3", Size: 30, CreatedAt: createdAt},
		{PartNumber: 4, Etag: "etag-4", Size: 40, CreatedAt: createdAt},
	}

	got := buildS3ListPartsResp(req, parts)

	if !got.IsTruncated {
		t.Fatal("IsTruncated = false, want true")
	}
	if got.NextPartNumberMarker != 3 {
		t.Fatalf("NextPartNumberMarker = %d, want 3", got.NextPartNumberMarker)
	}
	if len(got.Parts) != 2 || got.Parts[0].PartNumber != 2 || got.Parts[1].PartNumber != 3 {
		t.Fatalf("Parts = %+v, want part 2 and 3", got.Parts)
	}
	if got.Parts[0].ETag != `"etag-2"` {
		t.Fatalf("ETag = %q, want quoted etag", got.Parts[0].ETag)
	}
	if got.Parts[0].LastModified != "2026-05-24T01:02:03Z" {
		t.Fatalf("LastModified = %q, want RFC3339 UTC", got.Parts[0].LastModified)
	}
}

func TestBuildS3ListPartsRespCapsMaxParts(t *testing.T) {
	req := &dto.S3ListPartsReq{
		BucketName: "demo",
		ObjectKey:  "large.bin",
		UploadID:   "upload-1",
		MaxParts:   5000,
	}

	got := buildS3ListPartsResp(req, nil)

	if got.MaxParts != 1000 {
		t.Fatalf("MaxParts = %d, want 1000", got.MaxParts)
	}
	if got.IsTruncated {
		t.Fatal("IsTruncated = true, want false")
	}
}
