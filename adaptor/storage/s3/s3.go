package s3

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/url"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"oss/adaptor/storage"
	"oss/config"
)

const s3StorageScheme = "s3://"

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
	if endpoint != "" {
		loadOptions = append(loadOptions, awsconfig.WithEndpointResolverWithOptions(aws.EndpointResolverWithOptionsFunc(
			func(service, region string, options ...interface{}) (aws.Endpoint, error) {
				return aws.Endpoint{
					URL:               endpoint,
					SigningRegion:     cfg.Region,
					HostnameImmutable: true,
				}, nil
			})))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = cfg.ForcePathStyle
	})

	return &S3Storage{
		client:     client,
		rootBucket: strings.TrimSpace(cfg.Bucket),
	}, nil
}

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
		Sha256:      fmt.Sprintf("%x", h.sha256.Sum(nil)),
		Size:        h.bytesRead,
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

func (s *S3Storage) PutPart(ctx context.Context, bucket, uploadID string, partNumber int32, src io.Reader) (*storage.PutResult, error) {
	physicalBucket, partKey, err := s.buildPartLocator(bucket, uploadID, partNumber)
	if err != nil {
		return nil, err
	}

	h := newHashingReader(src)
	output, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(physicalBucket),
		Key:    aws.String(partKey),
		Body:   h,
	})
	if err != nil {
		return nil, fmt.Errorf("put part %s/%s: %w", physicalBucket, partKey, err)
	}

	return &storage.PutResult{
		StoragePath: formatStoragePath(physicalBucket, partKey),
		Etag:        trimETag(output.ETag),
		Sha256:      fmt.Sprintf("%x", h.sha256.Sum(nil)),
		Size:        h.bytesRead,
	}, nil
}

func (s *S3Storage) DeletePart(ctx context.Context, bucket, uploadID string, partNum int32) error {
	physicalBucket, partKey, err := s.buildPartLocator(bucket, uploadID, partNum)
	if err != nil {
		return err
	}
	_, err = s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(physicalBucket),
		Key:    aws.String(partKey),
	})
	if err != nil {
		return fmt.Errorf("delete part %s/%s: %w", physicalBucket, partKey, err)
	}
	return nil
}

func (s *S3Storage) DeleteParts(ctx context.Context, bucket, uploadID string) error {
	physicalBucket, prefix, err := s.buildMultipartPrefix(bucket, uploadID)
	if err != nil {
		return err
	}
	return s.deleteObjectsWithPrefix(ctx, physicalBucket, prefix)
}

func (s *S3Storage) MergeParts(ctx context.Context, bucket, objectKey string, version string, partPaths []string) (*storage.PutResult, error) {
	if len(partPaths) == 0 {
		return nil, fmt.Errorf("no part paths provided")
	}

	readers := make([]io.Reader, 0, len(partPaths))
	closers := make([]io.Closer, 0, len(partPaths))
	defer func() {
		for _, c := range closers {
			_ = c.Close()
		}
	}()

	for _, storagePath := range partPaths {
		rc, err := s.Get(ctx, storagePath)
		if err != nil {
			return nil, err
		}
		readers = append(readers, rc)
		closers = append(closers, rc)
	}

	return s.Put(ctx, bucket, objectKey, version, io.MultiReader(readers...))
}

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
		Sha256:      fmt.Sprintf("%x", h.sha256.Sum(nil)),
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
		if strings.HasPrefix(relativeKey, "/") {
			relativeKey = strings.TrimPrefix(relativeKey, "/")
		}
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

func (s *S3Storage) buildPartLocator(bucket, uploadID string, partNumber int32) (string, string, error) {
	if err := validateBucket(bucket); err != nil {
		return "", "", err
	}
	if strings.TrimSpace(uploadID) == "" {
		return "", "", fmt.Errorf("upload id is required")
	}
	partKey := fmt.Sprintf("multipart/%s/part_%d", uploadID, partNumber)
	if s.rootBucket != "" {
		partKey = path.Join(bucket, partKey)
	}
	return s.effectiveBucket(bucket), partKey, nil
}

func (s *S3Storage) buildMultipartPrefix(bucket, uploadID string) (string, string, error) {
	if err := validateBucket(bucket); err != nil {
		return "", "", err
	}
	if strings.TrimSpace(uploadID) == "" {
		return "", "", fmt.Errorf("upload id is required")
	}
	prefix := fmt.Sprintf("multipart/%s/", uploadID)
	if s.rootBucket != "" {
		prefix = path.Join(bucket, prefix)
	}
	return s.effectiveBucket(bucket), prefix, nil
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

func cleanObjectKey(objectKey string) (string, error) {
	if objectKey == "" {
		return "", fmt.Errorf("object key is required")
	}
	normalized := strings.ReplaceAll(objectKey, `\\`, "/")
	if strings.HasPrefix(normalized, "/") || strings.HasPrefix(normalized, "\\") {
		return "", fmt.Errorf("invalid object key: %s", objectKey)
	}
	segments := strings.Split(normalized, "/")
	for _, segment := range segments {
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
	segments := strings.Split(normalized, "/")
	for _, segment := range segments {
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

func isNotFound(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code := apiErr.ErrorCode()
		return code == "NotFound" || code == "NoSuchKey" || code == "NotFoundException" || code == "NoSuchBucket"
	}
	return false
}

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
			return nil, fmt.Errorf("list objects prefix %s/%s: %w", bucket, prefix, err)
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
			return fmt.Errorf("delete objects %s prefix: %w", bucket, err)
		}
	}
	return nil
}

func trimETag(etag *string) string {
	if etag == nil {
		return ""
	}
	s := strings.Trim(*etag, `"`)
	return s
}

type hashingReader struct {
	src       io.Reader
	md5       hash.Hash
	sha256    hash.Hash
	bytesRead int64
}

func newHashingReader(src io.Reader) *hashingReader {
	return &hashingReader{
		src:    src,
		md5:    md5.New(),
		sha256: sha256.New(),
	}
}

func (r *hashingReader) Read(p []byte) (int, error) {
	n, err := r.src.Read(p)
	if n > 0 {
		_, _ = r.md5.Write(p[:n])
		_, _ = r.sha256.Write(p[:n])
		r.bytesRead += int64(n)
	}
	return n, err
}

func (r *hashingReader) Etag() string {
	return hex.EncodeToString(r.md5.Sum(nil))
}
