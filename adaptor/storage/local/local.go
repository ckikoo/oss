package local

import (
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"oss/adaptor/storage"
	"oss/consts"
	storage_tool "oss/utils/storage"
	"oss/utils/tools"
)

// LocalStorage 本地磁盘存储实现
// 目录结构：
//
//	{baseDir}/{bucket}/{objectKey}_{version}                     ← 普通对象
//	{baseDir}/{bucket}/multipart/{uploadID}/part_{partNumber}   ← 分片临时文件
//	{baseDir}/{bucket}/_video/{transcodeID}/{profile}/...       ← 视频派生资产
type LocalStorage struct {
	baseDir string
}

var _ storage.IStorage = (*LocalStorage)(nil)

func New(baseDir string) storage.IStorage {
	if baseDir == "" {
		baseDir = "./storage"
	}
	return &LocalStorage{baseDir: baseDir}
}

// ===================== 普通对象操作 =====================

func (s *LocalStorage) Put(ctx context.Context, bucket, objectKey string, version string, src io.Reader) (*storage.PutResult, error) {
	destPath := s.buildObjectPath(bucket, objectKey, version)
	return saveAndHash(src, destPath)
}

func (s *LocalStorage) Get(ctx context.Context, storagePath string) (io.ReadCloser, error) {
	return os.Open(storagePath)
}

func (s *LocalStorage) Stat(ctx context.Context, storagePath string) (*storage.StatResult, error) {
	_, err := os.Stat(storagePath)
	if os.IsNotExist(err) {
		return &storage.StatResult{Exist: false}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat file %s: %w", storagePath, err)
	}
	return &storage.StatResult{Exist: true}, nil
}

func (s *LocalStorage) Delete(ctx context.Context, storagePath string) error {
	err := os.Remove(storagePath)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *LocalStorage) Copy(ctx context.Context, srcStoragePath string, dstBucket, dstKey, dstVersion string) (*storage.PutResult, error) {
	target := s.buildObjectPath(dstBucket, dstKey, dstVersion)

	srcFile, err := os.Open(srcStoragePath)
	if err != nil {
		return nil, fmt.Errorf("open source file %s: %w", srcStoragePath, err)
	}
	defer srcFile.Close()

	return saveAndHash(srcFile, target)
}

// ===================== 分片上传 =====================

// CreateMultipartUpload 初始化本地分片上传会话，创建临时目录并返回 storageUploadID。
// storageUploadID 格式：bucket{bucket}/{objectKey}_{version}/{id}
func (s *LocalStorage) CreateMultipartUpload(_ context.Context, bucket, objectKey string, version string) (string, error) {
	if err := validateBucket(bucket); err != nil {
		return "", err
	}
	uploadID := tools.UUIDHex()
	dirPath := s.buildMultipartDir(bucket, uploadID)
	if err := os.MkdirAll(dirPath, consts.FilePermDir); err != nil {
		return "", fmt.Errorf("create multipart dir %s: %w", dirPath, err)
	}
	return storage_tool.FormatUploadID(bucket, objectKey, version, uploadID), nil
}

// PutPart 将单个分片写入临时目录。
// 返回的 PutResult.Etag 须原样保存，CompleteMultipartUpload 时需传回（本地实现不校验 ETag，但保持接口一致）。
func (s *LocalStorage) PutPart(_ context.Context, storageUploadID string, partNumber int32, src io.Reader) (*storage.PutResult, error) {
	info, err := storage_tool.ParseUploadID(storageUploadID)
	if err != nil {
		return nil, err
	}
	partPath := s.buildPartPath(info.Bucket, info.UploadID, partNumber)
	return saveAndHash(src, partPath)
}

// CompleteMultipartUpload 按 PartNumber 顺序合并分片到最终目标文件，并清理临时目录。
// parts 无需预先排序，内部自动按 PartNumber 升序处理。
func (s *LocalStorage) CompleteMultipartUpload(ctx context.Context, storageUploadID string, parts []storage.PartInfo) (*storage.PutResult, error) {
	if len(parts) == 0 {
		return nil, fmt.Errorf("complete multipart upload: no parts provided")
	}

	info, err := storage_tool.ParseUploadID(storageUploadID)
	if err != nil {
		return nil, err
	}

	// 按 PartNumber 升序排列
	sorted := make([]storage.PartInfo, len(parts))
	copy(sorted, parts)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].PartNumber < sorted[j].PartNumber
	})

	for i, part := range sorted {
		expected := int32(i + 1)
		if part.PartNumber != expected {
			err := fmt.Errorf("part number not continuous: got=%d want=%d", part.PartNumber, expected)
			return nil, err
		}
	}

	// 打开所有分片文件
	readers := make([]io.Reader, 0, len(sorted))
	closers := make([]io.Closer, 0, len(sorted))
	defer func() {
		for _, c := range closers {
			_ = c.Close()
		}
	}()

	for _, p := range sorted {
		partPath := s.buildPartPath(info.Bucket, info.UploadID, p.PartNumber)
		f, err := os.Open(partPath)
		if err != nil {
			return nil, fmt.Errorf("open part %d: %w", p.PartNumber, err)
		}
		readers = append(readers, f)
		closers = append(closers, f)
	}

	destPath := s.buildObjectPath(info.Bucket, info.UploadID, info.Version)
	result, err := saveAndHash(io.MultiReader(readers...), destPath)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// AbortUpload 取消分片上传，删除临时目录及所有已上传的分片。
// 幂等：目录不存在时不报错。
func (s *LocalStorage) AbortUpload(_ context.Context, storageUploadID string) error {
	info, err := storage_tool.ParseUploadID(storageUploadID)
	if err != nil {
		return err
	}
	return s.removeMultipartDir(info.Bucket, info.UploadID)
}

func (s *LocalStorage) removeMultipartDir(bucket, uploadID string) error {
	dirPath := s.buildMultipartDir(bucket, uploadID)
	err := os.RemoveAll(dirPath)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// ===================== 视频派生资产 =====================

func (s *LocalStorage) PutAsset(_ context.Context, bucket string, assetKey string, src io.Reader) (*storage.PutResult, error) {
	destPath, err := s.buildAssetPath(bucket, assetKey)
	if err != nil {
		return nil, err
	}
	return saveAndHash(src, destPath)
}

func (s *LocalStorage) GetAsset(_ context.Context, bucket string, assetKey string) (io.ReadCloser, error) {
	assetPath, err := s.buildAssetPath(bucket, assetKey)
	if err != nil {
		return nil, err
	}
	return os.Open(assetPath)
}

func (s *LocalStorage) DeleteAsset(_ context.Context, bucket string, assetKey string) error {
	assetPath, err := s.buildAssetPath(bucket, assetKey)
	if err != nil {
		return err
	}
	err = os.Remove(assetPath)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *LocalStorage) DeleteAssetPrefix(_ context.Context, bucket string, prefix string) error {
	prefixPath, err := s.buildAssetPath(bucket, prefix)
	if err != nil {
		return err
	}
	err = os.RemoveAll(prefixPath)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *LocalStorage) MoveAssetPrefix(_ context.Context, bucket string, srcPrefix string, dstPrefix string) error {
	srcPath, err := s.buildAssetPath(bucket, srcPrefix)
	if err != nil {
		return err
	}
	dstPath, err := s.buildAssetPath(bucket, dstPrefix)
	if err != nil {
		return err
	}
	if srcPath == dstPath {
		return nil
	}
	if pathNested(srcPath, dstPath) {
		return fmt.Errorf("destination prefix cannot be inside source prefix")
	}
	if _, err := os.Stat(srcPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("asset prefix not found: %s", srcPrefix)
		}
		return err
	}
	if err := os.MkdirAll(path.Dir(dstPath), consts.FilePermDir); err != nil {
		return fmt.Errorf("mkdir %s: %w", path.Dir(dstPath), err)
	}
	if pathNested(dstPath, srcPath) {
		return s.moveNestedAssetPrefix(srcPath, dstPath)
	}
	if err := os.RemoveAll(dstPath); err != nil {
		return fmt.Errorf("remove existing asset prefix %s: %w", dstPrefix, err)
	}
	if err := os.Rename(srcPath, dstPath); err != nil {
		return fmt.Errorf("move asset prefix %s to %s: %w", srcPrefix, dstPrefix, err)
	}
	return nil
}

func (s *LocalStorage) moveNestedAssetPrefix(srcPath string, dstPath string) error {
	tempPath, err := os.MkdirTemp(path.Dir(dstPath), ".asset-move-*")
	if err != nil {
		return fmt.Errorf("create temp dir for asset move: %w", err)
	}
	if err := os.Remove(tempPath); err != nil {
		return fmt.Errorf("remove temp placeholder: %w", err)
	}
	if err := os.Rename(srcPath, tempPath); err != nil {
		return fmt.Errorf("move to temp path: %w", err)
	}
	if err := os.RemoveAll(dstPath); err != nil {
		return fmt.Errorf("remove existing nested prefix: %w", err)
	}
	if err := os.Rename(tempPath, dstPath); err != nil {
		return fmt.Errorf("move temp to destination: %w", err)
	}
	return nil
}

// ===================== 路径构建 =====================

func (s *LocalStorage) buildObjectPath(bucket, objectKey string, version string) string {
	if version == "" {
		return path.Join(s.baseDir, bucket, objectKey)
	}
	return path.Join(s.baseDir, bucket, objectKey+"_"+version)
}

func (s *LocalStorage) buildMultipartDir(bucket, uploadID string) string {
	return path.Join(s.baseDir, bucket, "multipart", uploadID)
}

func (s *LocalStorage) buildPartPath(bucket, uploadID string, partNumber int32) string {
	return path.Join(s.buildMultipartDir(bucket, uploadID), fmt.Sprintf("part_%d", partNumber))
}

func (s *LocalStorage) buildAssetPath(bucket string, assetKey string) (string, error) {
	if err := validateBucket(bucket); err != nil {
		return "", err
	}
	cleanKey, err := cleanAssetKey(assetKey)
	if err != nil {
		return "", err
	}
	return path.Join(s.baseDir, bucket, cleanKey), nil
}

// ===================== 校验工具 =====================

func validateBucket(bucket string) error {
	if bucket == "" {
		return fmt.Errorf("bucket is required")
	}
	if bucket == "." || bucket == ".." || path.IsAbs(bucket) || strings.ContainsAny(bucket, `/\`) {
		return fmt.Errorf("invalid bucket: %s", bucket)
	}
	return nil
}

func cleanAssetKey(assetKey string) (string, error) {
	if assetKey == "" {
		return "", fmt.Errorf("asset key is required")
	}
	if strings.HasPrefix(assetKey, "/") || strings.HasPrefix(assetKey, `\`) || path.IsAbs(assetKey) {
		return "", fmt.Errorf("invalid asset key: %s", assetKey)
	}
	normalized := strings.ReplaceAll(assetKey, `\`, "/")
	for _, segment := range strings.Split(normalized, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("invalid asset key: %s", assetKey)
		}
	}
	cleanKey := path.Clean(filepath.FromSlash(normalized))
	if cleanKey == "." || strings.HasPrefix(cleanKey, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid asset key: %s", assetKey)
	}
	return cleanKey, nil
}

func pathNested(parent string, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// ===================== IO 工具 =====================

var copyBufPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 32*1024)
		return &buf
	},
}

// saveAndHash 创建目录、写文件，同时流式计算 MD5 和 SHA256。
func saveAndHash(src io.Reader, destPath string) (*storage.PutResult, error) {
	if err := os.MkdirAll(path.Dir(destPath), consts.FilePermDir); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", path.Dir(destPath), err)
	}

	dst, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, consts.FilePermFile)
	if err != nil {
		return nil, fmt.Errorf("create file %s: %w", destPath, err)
	}
	defer dst.Close()

	fi, statErr := dst.Stat()
	isRegular := statErr == nil && fi.Mode().IsRegular()

	md5Hasher := md5.New()
	mw := io.MultiWriter(dst, md5Hasher)

	bufp := copyBufPool.Get().(*[]byte)
	defer copyBufPool.Put(bufp)

	size, err := io.CopyBuffer(mw, src, *bufp)
	if err != nil {
		if isRegular {
			_ = os.Remove(destPath)
		}
		return nil, fmt.Errorf("write file %s: %w", destPath, err)
	}

	return &storage.PutResult{
		StoragePath: destPath,
		Etag:        fmt.Sprintf("%x", md5Hasher.Sum(nil)),
		Size:        size,
	}, nil
}
