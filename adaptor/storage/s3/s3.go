package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"sort"
	"strings"

	"oss/adaptor/storage"
	"oss/config"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

const (
	s3StorageScheme   = "s3://"
	s3MultipartScheme = "s3mp://"
)

type S3Storage struct {
	client     *s3.Client
	rootBucket string
}

func New(cfg config.S3Storage) (storage.IStorage, error) {
	if strings.TrimSpace(cfg.Region) == "" {
		return nil, fmt.Errorf("storage.s3.region is required")
	}
	if strings.TrimSpace(cfg.AccessKeyID) == "" {
		return nil, fmt.Errorf("storage.s3.access_key_id is required")
	}
	if strings.TrimSpace(cfg.SecretAccessKey) == "" {
		return nil, fmt.Errorf("storage.s3.secret_access_key is required")
	}

	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint != "" {
		parsed, err := url.Parse(endpoint)
		if err != nil {
			return nil, fmt.Errorf("storage.s3.endpoint invalid: %w", err)
		}
		if parsed.Scheme == "" {
			endpoint = "https://" + endpoint
		}
	}

	loadOptions := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")),
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = cfg.ForcePathStyle
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	})

	return &S3Storage{
		client:     client,
		rootBucket: strings.TrimSpace(cfg.Bucket),
	}, nil
}

// ===================== 普通对象操作 =====================

func (s *S3Storage) Put(ctx context.Context, bucket, objectKey string, version string, src io.Reader) (*storage.PutResult, error) {
	physicalBucket, physicalKey, err := s.buildObjectLocator(bucket, objectKey, version)
	if err != nil {
		return nil, err
	}

	h := newHashingReader(src)
	output, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(physicalBucket),
		Key:    aws.String(physicalKey),
		Body:   h,
	})
	if err != nil {
		return nil, fmt.Errorf("put object %s/%s: %w", physicalBucket, physicalKey, err)
	}

	return &storage.PutResult{
		StoragePath: formatStoragePath(physicalBucket, physicalKey),
		Etag:        trimETag(output.ETag),
		Size:        *output.Size,
	}, nil
}

func (s *S3Storage) Get(ctx context.Context, storagePath string) (io.ReadCloser, error) {
	physicalBucket, physicalKey, err := parseStoragePath(storagePath)
	if err != nil {
		return nil, err
	}
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(physicalBucket),
		Key:    aws.String(physicalKey),
	})
	if err != nil {
		return nil, fmt.Errorf("get object %s/%s: %w", physicalBucket, physicalKey, err)
	}
	return output.Body, nil
}

func (s *S3Storage) Stat(ctx context.Context, storagePath string) (*storage.StatResult, error) {
	physicalBucket, physicalKey, err := parseStoragePath(storagePath)
	if err != nil {
		return nil, err
	}
	_, err = s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(physicalBucket),
		Key:    aws.String(physicalKey),
	})
	if err != nil {
		if isNotFound(err) {
			return &storage.StatResult{Exist: false}, nil
		}
		return nil, fmt.Errorf("stat object %s/%s: %w", physicalBucket, physicalKey, err)
	}
	return &storage.StatResult{Exist: true}, nil
}

func (s *S3Storage) Delete(ctx context.Context, storagePath string) error {
	physicalBucket, physicalKey, err := parseStoragePath(storagePath)
	if err != nil {
		return err
	}
	_, err = s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(physicalBucket),
		Key:    aws.String(physicalKey),
	})
	if err != nil {
		return fmt.Errorf("delete object %s/%s: %w", physicalBucket, physicalKey, err)
	}
	return nil
}

const (
	copyPartSize  = 64 * 1024 * 1024       // 64MB 每片
	maxSingleCopy = 5 * 1024 * 1024 * 1024 // 5GB，超过走分片
)

func (s *S3Storage) Copy(ctx context.Context, srcStoragePath string, dstBucket, dstKey, dstVersion string) (*storage.PutResult, error) {
	srcBucket, srcKey, err := parseStoragePath(srcStoragePath)
	if err != nil {
		return nil, fmt.Errorf("copy: invalid src path: %w", err)
	}

	// 获取源对象大小
	head, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(srcBucket),
		Key:    aws.String(srcKey),
	})
	if err != nil {
		return nil, fmt.Errorf("copy: head src object: %w", err)
	}
	size := int64(0)
	if head.ContentLength != nil {
		size = *head.ContentLength
	}

	if size <= maxSingleCopy {
		return s.copyObjectSingle(ctx, srcBucket, srcKey, dstBucket, dstKey, dstVersion)
	}
	return s.copyObjectMultipart(ctx, srcBucket, srcKey, size, dstBucket, dstKey, dstVersion)
}

// copyObjectSingle 直接 CopyObject，适用于 <= 5GB
func (s *S3Storage) copyObjectSingle(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey, dstVersion string) (*storage.PutResult, error) {
	physicalDstBucket, physicalDstKey, err := s.buildObjectLocator(dstBucket, dstKey, dstVersion)
	if err != nil {
		return nil, err
	}

	copySource := fmt.Sprintf("%s/%s", srcBucket, url.PathEscape(srcKey))
	output, err := s.client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(physicalDstBucket),
		Key:        aws.String(physicalDstKey),
		CopySource: aws.String(copySource),
	})
	if err != nil {
		return nil, fmt.Errorf("copy object %s/%s: %w", srcBucket, srcKey, err)
	}

	var size int64
	head, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(physicalDstBucket),
		Key:    aws.String(physicalDstKey),
	})
	if err == nil && head.ContentLength != nil {
		size = *head.ContentLength
	}

	return &storage.PutResult{
		StoragePath: formatStoragePath(physicalDstBucket, physicalDstKey),
		Etag:        trimETag(output.CopyObjectResult.ETag),
		Size:        size,
	}, nil
}

// copyObjectMultipart 分片 CopyPart，适用于 > 5GB
func (s *S3Storage) copyObjectMultipart(ctx context.Context, srcBucket, srcKey string, srcSize int64, dstBucket, dstKey, dstVersion string) (*storage.PutResult, error) {
	physicalDstBucket, physicalDstKey, err := s.buildObjectLocator(dstBucket, dstKey, dstVersion)
	if err != nil {
		return nil, err
	}

	uploadOutput, err := s.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(physicalDstBucket),
		Key:    aws.String(physicalDstKey),
	})
	if err != nil {
		return nil, fmt.Errorf("copy multipart create: %w", err)
	}
	uploadID := *uploadOutput.UploadId

	abort := func() {
		_, _ = s.client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
			Bucket:   aws.String(physicalDstBucket),
			Key:      aws.String(physicalDstKey),
			UploadId: aws.String(uploadID),
		})
	}

	copySource := fmt.Sprintf("%s/%s", srcBucket, url.PathEscape(srcKey))
	var completedParts []types.CompletedPart
	var offset int64
	partNumber := int32(1)

	for offset < srcSize {
		end := offset + copyPartSize - 1
		if end >= srcSize {
			end = srcSize - 1
		}

		output, err := s.client.UploadPartCopy(ctx, &s3.UploadPartCopyInput{
			Bucket:          aws.String(physicalDstBucket),
			Key:             aws.String(physicalDstKey),
			UploadId:        aws.String(uploadID),
			PartNumber:      aws.Int32(partNumber),
			CopySource:      aws.String(copySource),
			CopySourceRange: aws.String(fmt.Sprintf("bytes=%d-%d", offset, end)),
		})
		if err != nil {
			abort()
			return nil, fmt.Errorf("copy part %d: %w", partNumber, err)
		}

		completedParts = append(completedParts, types.CompletedPart{
			PartNumber: aws.Int32(partNumber),
			ETag:       output.CopyPartResult.ETag,
		})

		offset += copyPartSize
		partNumber++
	}

	completeOutput, err := s.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(physicalDstBucket),
		Key:      aws.String(physicalDstKey),
		UploadId: aws.String(uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: completedParts,
		},
	})

	if err != nil {
		abort()
		return nil, fmt.Errorf("copy multipart complete: %w", err)
	}

	return &storage.PutResult{
		StoragePath: formatStoragePath(physicalDstBucket, physicalDstKey),
		Etag:        trimETag(completeOutput.ETag),
		Size:        srcSize,
	}, nil
}

// ===================== 原生分片上传 =====================

// CreateMultipartUpload 初始化 S3 原生分片上传，返回 storageUploadID。
// storageUploadID 格式：s3mp://bucket/physicalKey?id=<s3UploadId>
// 调用方须持久化此 ID，用于后续 PutPart / CompleteMultipartUpload / AbortUpload。
func (s *S3Storage) CreateMultipartUpload(ctx context.Context, bucket, objectKey string, version string) (string, error) {
	physicalBucket, physicalKey, err := s.buildObjectLocator(bucket, objectKey, version)
	if err != nil {
		return "", err
	}

	output, err := s.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(physicalBucket),
		Key:    aws.String(physicalKey),
	})
	if err != nil {
		return "", fmt.Errorf("create multipart upload %s/%s: %w", physicalBucket, physicalKey, err)
	}
	if output.UploadId == nil {
		return "", fmt.Errorf("create multipart upload %s/%s: empty upload id", physicalBucket, physicalKey)
	}

	return *output.UploadId, nil
}

// PutPart 上传单个分片，对应 S3 UploadPart。
// 返回的 PutResult.Etag 须原样保存，CompleteMultipartUpload 时需传回。
// 注意：S3 要求每个分片（除最后一片）不小于 5 MB。
func (s *S3Storage) PutPart(ctx context.Context, bucket, objectKey, version string, storageUploadID string, partNumber int32, src io.Reader) (*storage.PutResult, error) {

	h := newHashingReader(src)
	output, err := s.client.UploadPart(ctx, &s3.UploadPartInput{
		Bucket:     aws.String(bucket),
		Key:        aws.String(objectKey),
		UploadId:   aws.String(storageUploadID),
		PartNumber: aws.Int32(partNumber),
		Body:       h,
	})
	if err != nil {
		return nil, fmt.Errorf("upload part %d for %s/%s: %w", partNumber, bucket, objectKey, err)
	}

	return &storage.PutResult{
		Etag: trimETag(output.ETag),
		Size: h.bytesRead,
	}, nil
}

// CompleteMultipartUpload 通知 S3 服务端合并所有分片，完成上传。
// parts 内的 ETag 必须来自对应 PutPart 的返回值，PartNumber 须升序。
// 说明：
//   - 返回的 Etag 为 S3 复合 ETag（格式 md5hash-N），非内容 MD5。
//   - 返回的 Sha256 为空，调用方应在上传各分片时自行累积计算。
//   - Size 通过 HeadObject 获取，产生一次额外请求。
func (s *S3Storage) CompleteMultipartUpload(ctx context.Context, bucket, objectKey, version string, storageUploadID string, parts []storage.PartInfo) (*storage.PutResult, error) {
	if len(parts) == 0 {
		return nil, fmt.Errorf("complete multipart upload: no parts provided")
	}

	// 按 PartNumber 升序排列（S3 要求）
	sorted := make([]storage.PartInfo, len(parts))
	copy(sorted, parts)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].PartNumber < sorted[j].PartNumber
	})

	completedParts := make([]types.CompletedPart, len(sorted))
	for i, p := range sorted {
		etag := p.ETag
		completedParts[i] = types.CompletedPart{
			PartNumber: aws.Int32(p.PartNumber),
			ETag:       aws.String(etag),
		}
	}

	output, err := s.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(objectKey),
		UploadId: aws.String(storageUploadID),
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: completedParts,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("complete multipart upload %s/%s: %w", bucket, objectKey, err)
	}

	storagePath := formatStoragePath(bucket, objectKey)

	// CompleteMultipartUpload 不返回 ContentLength，HeadObject 补齐
	var size int64
	head, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(objectKey),
	})
	if err == nil && head.ContentLength != nil {
		size = *head.ContentLength
	}

	return &storage.PutResult{
		StoragePath: storagePath,
		Etag:        trimETag(output.ETag), // 复合 ETag，格式 hash-N
		Size:        size,
	}, nil
}

// AbortUpload 取消分片上传并清理已上传的所有分片。
// 幂等：uploadID 已完成或不存在时不报错。
func (s *S3Storage) AbortUpload(ctx context.Context, bucket, objectKey, version string, storageUploadID string) error {
	_, err := s.client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(objectKey),
		UploadId: aws.String(storageUploadID),
	})
	if err != nil {
		// NoSuchUpload 表示已完成或不存在，视为成功
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "NoSuchUpload" {
			return nil
		}
		return fmt.Errorf("abort multipart upload %s/%s: %w", bucket, objectKey, err)
	}
	return nil
}

// ===================== 视频派生资产 =====================

func (s *S3Storage) PutAsset(ctx context.Context, bucket string, assetKey string, src io.Reader) (*storage.PutResult, error) {
	physicalBucket, physicalKey, err := s.buildAssetLocator(bucket, assetKey)
	if err != nil {
		return nil, err
	}

	h := newHashingReader(src)
	output, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(physicalBucket),
		Key:    aws.String(physicalKey),
		Body:   h,
	})
	if err != nil {
		return nil, fmt.Errorf("put asset %s/%s: %w", physicalBucket, physicalKey, err)
	}

	return &storage.PutResult{
		StoragePath: formatStoragePath(physicalBucket, physicalKey),
		Etag:        trimETag(output.ETag),
		Size:        h.bytesRead,
	}, nil
}

func (s *S3Storage) GetAsset(ctx context.Context, bucket string, assetKey string) (io.ReadCloser, error) {
	physicalBucket, physicalKey, err := s.buildAssetLocator(bucket, assetKey)
	if err != nil {
		return nil, err
	}
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(physicalBucket),
		Key:    aws.String(physicalKey),
	})
	if err != nil {
		return nil, fmt.Errorf("get asset %s/%s: %w", physicalBucket, physicalKey, err)
	}
	return output.Body, nil
}

func (s *S3Storage) DeleteAsset(ctx context.Context, bucket string, assetKey string) error {
	physicalBucket, physicalKey, err := s.buildAssetLocator(bucket, assetKey)
	if err != nil {
		return err
	}
	_, err = s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(physicalBucket),
		Key:    aws.String(physicalKey),
	})
	if err != nil {
		return fmt.Errorf("delete asset %s/%s: %w", physicalBucket, physicalKey, err)
	}
	return nil
}

func (s *S3Storage) DeleteAssetPrefix(ctx context.Context, bucket string, prefix string) error {
	physicalBucket, physicalPrefix, err := s.buildAssetPrefix(bucket, prefix)
	if err != nil {
		return err
	}
	return s.deleteObjectsWithPrefix(ctx, physicalBucket, physicalPrefix)
}

func (s *S3Storage) MoveAssetPrefix(ctx context.Context, bucket string, srcPrefix string, dstPrefix string) error {
	if strings.TrimSpace(srcPrefix) == "" || strings.TrimSpace(dstPrefix) == "" {
		return fmt.Errorf("asset prefix cannot be empty")
	}
	if srcPrefix == dstPrefix {
		return nil
	}
	physicalBucket, physicalSrcPrefix, err := s.buildAssetPrefix(bucket, srcPrefix)
	if err != nil {
		return err
	}
	_, physicalDstPrefix, err := s.buildAssetPrefix(bucket, dstPrefix)
	if err != nil {
		return err
	}
	if strings.HasPrefix(physicalDstPrefix, physicalSrcPrefix) {
		return fmt.Errorf("destination prefix cannot be inside source prefix")
	}

	objects, err := s.listObjectsWithPrefix(ctx, physicalBucket, physicalSrcPrefix)
	if err != nil {
		return err
	}
	if len(objects) == 0 {
		return nil
	}

	for _, key := range objects {
		relativeKey := strings.TrimPrefix(key, physicalSrcPrefix)
		relativeKey = strings.TrimPrefix(relativeKey, "/")
		dstKey := path.Join(physicalDstPrefix, relativeKey)
		_, err := s.client.CopyObject(ctx, &s3.CopyObjectInput{
			Bucket:     aws.String(physicalBucket),
			CopySource: aws.String(fmt.Sprintf("%s/%s", physicalBucket, url.PathEscape(key))),
			Key:        aws.String(dstKey),
		})
		if err != nil {
			return fmt.Errorf("copy asset %s/%s to %s/%s: %w", physicalBucket, key, physicalBucket, dstKey, err)
		}
	}

	return s.deleteObjects(ctx, physicalBucket, objects)
}

// ===================== 路径构建 =====================

func (s *S3Storage) buildObjectLocator(bucket, objectKey string, version string) (string, string, error) {
	if err := validateBucket(bucket); err != nil {
		return "", "", err
	}
	objectKey, err := cleanObjectKey(objectKey)
	if err != nil {
		return "", "", err
	}
	if version != "" {
		objectKey = objectKey + "_" + version
	}
	return s.effectiveBucket(bucket), objectKey, nil
}

func (s *S3Storage) buildAssetLocator(bucket string, assetKey string) (string, string, error) {
	if err := validateBucket(bucket); err != nil {
		return "", "", err
	}
	assetKey, err := cleanAssetKey(assetKey)
	if err != nil {
		return "", "", err
	}
	if s.rootBucket != "" {
		assetKey = path.Join(bucket, assetKey)
	}
	return s.effectiveBucket(bucket), assetKey, nil
}

func (s *S3Storage) buildAssetPrefix(bucket string, prefix string) (string, string, error) {
	if err := validateBucket(bucket); err != nil {
		return "", "", err
	}
	prefix, err := cleanAssetKey(prefix)
	if err != nil {
		return "", "", err
	}
	if s.rootBucket != "" {
		prefix = path.Join(bucket, prefix)
	}
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return s.effectiveBucket(bucket), prefix, nil
}

func (s *S3Storage) effectiveBucket(bucket string) string {
	if s.rootBucket != "" {
		return s.rootBucket
	}
	return bucket
}

// ===================== 存储路径 =====================

func formatStoragePath(bucket, key string) string {
	return fmt.Sprintf("%s%s/%s", s3StorageScheme, bucket, key)
}

func parseStoragePath(storagePath string) (string, string, error) {
	if !strings.HasPrefix(storagePath, s3StorageScheme) {
		return "", "", fmt.Errorf("invalid s3 storage path: %s", storagePath)
	}
	trimmed := strings.TrimPrefix(storagePath, s3StorageScheme)
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid s3 storage path: %s", storagePath)
	}
	return parts[0], parts[1], nil
}

// ===================== Key 校验与清理 =====================

func cleanObjectKey(objectKey string) (string, error) {
	if objectKey == "" {
		return "", fmt.Errorf("object key is required")
	}
	normalized := strings.ReplaceAll(objectKey, `\\`, "/")
	if strings.HasPrefix(normalized, "/") || strings.HasPrefix(normalized, "\\") {
		return "", fmt.Errorf("invalid object key: %s", objectKey)
	}
	for _, segment := range strings.Split(normalized, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("invalid object key: %s", objectKey)
		}
	}
	cleanKey := path.Clean(normalized)
	if cleanKey == "." || strings.HasPrefix(cleanKey, "../") {
		return "", fmt.Errorf("invalid object key: %s", objectKey)
	}
	return cleanKey, nil
}

func cleanAssetKey(assetKey string) (string, error) {
	if assetKey == "" {
		return "", fmt.Errorf("asset key is required")
	}
	if strings.HasPrefix(assetKey, "/") || strings.HasPrefix(assetKey, "\\") {
		return "", fmt.Errorf("invalid asset key: %s", assetKey)
	}
	normalized := strings.ReplaceAll(assetKey, "\\", "/")
	for _, segment := range strings.Split(normalized, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("invalid asset key: %s", assetKey)
		}
	}
	cleanKey := path.Clean(normalized)
	if cleanKey == "." || strings.HasPrefix(cleanKey, "../") {
		return "", fmt.Errorf("invalid asset key: %s", assetKey)
	}
	return cleanKey, nil
}

func validateBucket(bucket string) error {
	if strings.TrimSpace(bucket) == "" {
		return fmt.Errorf("bucket is required")
	}
	if bucket == "." || bucket == ".." || strings.HasPrefix(bucket, "/") || strings.Contains(bucket, "\\") {
		return fmt.Errorf("invalid bucket: %s", bucket)
	}
	return nil
}

// ===================== 批量删除工具 =====================

func (s *S3Storage) deleteObjectsWithPrefix(ctx context.Context, bucket, prefix string) error {
	keys, err := s.listObjectsWithPrefix(ctx, bucket, prefix)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return nil
	}
	return s.deleteObjects(ctx, bucket, keys)
}

func (s *S3Storage) listObjectsWithPrefix(ctx context.Context, bucket, prefix string) ([]string, error) {
	var keys []string
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list objects %s/%s: %w", bucket, prefix, err)
		}
		for _, obj := range page.Contents {
			if obj.Key != nil {
				keys = append(keys, *obj.Key)
			}
		}
	}
	return keys, nil
}

func (s *S3Storage) deleteObjects(ctx context.Context, bucket string, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	const batchSize = 1000
	for i := 0; i < len(keys); i += batchSize {
		end := i + batchSize
		if end > len(keys) {
			end = len(keys)
		}
		objects := make([]types.ObjectIdentifier, 0, end-i)
		for _, key := range keys[i:end] {
			objects = append(objects, types.ObjectIdentifier{Key: aws.String(key)})
		}
		_, err := s.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(bucket),
			Delete: &types.Delete{Objects: objects, Quiet: aws.Bool(true)},
		})
		if err != nil {
			return fmt.Errorf("delete objects in %s: %w", bucket, err)
		}
	}
	return nil
}

// ===================== 错误判断 =====================

func isNotFound(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code := apiErr.ErrorCode()
		return code == "NotFound" || code == "NoSuchKey" || code == "NotFoundException" || code == "NoSuchBucket"
	}
	return false
}

// ===================== ETag 工具 =====================

func trimETag(etag *string) string {
	if etag == nil {
		return ""
	}
	return strings.Trim(*etag, `"`)
}

// ===================== hashingReader =====================

type hashingReader struct {
	src       io.Reader
	bytesRead int64
}

func newHashingReader(src io.Reader) *hashingReader {
	return &hashingReader{
		src: src,
	}
}

func (r *hashingReader) Read(p []byte) (int, error) {
	n, err := r.src.Read(p)
	if n > 0 {
		r.bytesRead += int64(n)
	}
	return n, err
}
