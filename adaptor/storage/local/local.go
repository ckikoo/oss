package local

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"oss/adaptor/storage"
	"oss/consts"
)

// LocalStorage 本地磁盘存储实现
// 目录结构：
//
//	{baseDir}/{bucket}/{objectKey}_{version}                               ← 普通对象
//	{baseDir}/{bucket}/multipart/{uploadID}/part_{partNumber}   ← 分片
//	{baseDir}/{bucket}/_video/{transcodeID}/{profile}/...       ← 视频派生资产
type LocalStorage struct {
	baseDir string
}

var _ storage.IStorage = (*LocalStorage)(nil)

// New 创建本地存储实例，baseDir 对应 config.Server.SaveDir
func New(baseDir string) storage.IStorage {
	if baseDir == "" {
		baseDir = "./storage"
	}
	return &LocalStorage{baseDir: baseDir}
}

// Put 保存普通对象到磁盘，一次 IO 同时计算 MD5 和 SHA256
func (s *LocalStorage) Put(ctx context.Context, bucket, objectKey string, version string, src io.Reader) (*storage.PutResult, error) {
	destPath := s.BuildObjectPath(ctx, bucket, objectKey, version)
	return saveAndHash(src, destPath)
}

// Get 打开文件返回 ReadCloser，调用方负责 Close
func (s *LocalStorage) Get(ctx context.Context, storagePath string) (io.ReadCloser, error) {
	if _, err := os.Stat(storagePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("file not found: %s", storagePath)
	}
	return os.Open(storagePath)
}

// Delete 删除单个文件，文件不存在时静默忽略
func (s *LocalStorage) Delete(ctx context.Context, storagePath string) error {
	err := os.Remove(storagePath)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// PutPart 保存分片到专属目录
func (s *LocalStorage) PutPart(ctx context.Context, bucket, uploadID string, partNumber int32, src io.Reader) (*storage.PutResult, error) {
	destPath := s.buildPartPath(bucket, uploadID, partNumber)
	return saveAndHash(src, destPath)
}

// DeletePart 删除某次分片上传的单个分片目录
func (s *LocalStorage) DeletePart(ctx context.Context, bucket, uploadID string, partNum int32) error {
	partPath := s.buildPartPath(bucket, uploadID, partNum)
	err := os.Remove(partPath)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// DeleteParts 删除整个分片上传目录（AbortMultipartUpload / CompleteMultipartUpload 后清理）
func (s *LocalStorage) DeleteParts(ctx context.Context, bucket, uploadID string) error {
	dirPath := path.Join(s.baseDir, bucket, "multipart", uploadID)
	err := os.RemoveAll(dirPath)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// PutAsset 保存视频派生资产到专属前缀。
func (s *LocalStorage) PutAsset(ctx context.Context, bucket string, assetKey string, src io.Reader) (*storage.PutResult, error) {
	destPath, err := s.buildAssetPath(bucket, assetKey)
	if err != nil {
		return nil, err
	}
	return saveAndHash(src, destPath)
}

// GetAsset 打开视频派生资产，调用方负责 Close。
func (s *LocalStorage) GetAsset(ctx context.Context, bucket string, assetKey string) (io.ReadCloser, error) {
	assetPath, err := s.buildAssetPath(bucket, assetKey)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(assetPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("asset not found: %s", assetKey)
	}
	return os.Open(assetPath)
}

// DeleteAsset 删除单个视频派生资产，文件不存在时静默忽略。
func (s *LocalStorage) DeleteAsset(ctx context.Context, bucket string, assetKey string) error {
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

// DeleteAssetPrefix 删除视频派生资产前缀。
func (s *LocalStorage) DeleteAssetPrefix(ctx context.Context, bucket string, prefix string) error {
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

// MoveAssetPrefix 将视频派生资产前缀整体移动到新前缀。
func (s *LocalStorage) MoveAssetPrefix(ctx context.Context, bucket string, srcPrefix string, dstPrefix string) error {
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
		return fmt.Errorf("create temporary asset move path: %w", err)
	}
	if err := os.Remove(tempPath); err != nil {
		return fmt.Errorf("remove temporary asset move path: %w", err)
	}

	if err := os.Rename(srcPath, tempPath); err != nil {
		return fmt.Errorf("move nested asset prefix to temporary path: %w", err)
	}
	if err := os.RemoveAll(dstPath); err != nil {
		return fmt.Errorf("remove existing nested asset prefix: %w", err)
	}
	if err := os.Rename(tempPath, dstPath); err != nil {
		return fmt.Errorf("move nested asset prefix to destination: %w", err)
	}
	return nil
}

// MergeParts：用 MultiReader 把分片串成单流，复用 saveAndHash
func (s *LocalStorage) MergeParts(ctx context.Context, bucket, objectKey string, version string, partPaths []string) (*storage.PutResult, error) {
	if len(partPaths) == 0 {
		return nil, fmt.Errorf("no part paths provided")
	}

	files := make([]io.Reader, 0, len(partPaths))
	closers := make([]io.Closer, 0, len(partPaths))
	defer func() {
		for _, c := range closers {
			_ = c.Close()
		}
	}()

	for _, p := range partPaths {
		f, err := os.Open(p)
		if err != nil {
			return nil, fmt.Errorf("open part %s: %w", p, err)
		}
		files = append(files, f)
		closers = append(closers, f)
	}

	// 多个文件 → 一个流，saveAndHash 完全不用改
	return saveAndHash(io.MultiReader(files...), s.BuildObjectPath(ctx, bucket, objectKey, version))
}

// BuildObjectPath 返回普通对象的完整磁盘路径（供外部记录到 DB）
func (s *LocalStorage) BuildObjectPath(ctx context.Context, bucket, objectKey string, version string) string {
	if version == "" {
		return path.Join(s.baseDir, bucket, objectKey)
	}

	return path.Join(s.baseDir, bucket, objectKey+"_"+version)
}

// ---- 内部辅助 ----

func (s *LocalStorage) buildPartPath(bucket, uploadID string, partNumber int32) string {
	return path.Join(s.baseDir, bucket, "multipart", uploadID, fmt.Sprintf("part_%d", partNumber))
}

func (s *LocalStorage) buildAssetPath(bucket string, assetKey string) (string, error) {
	if err := validateAssetBucket(bucket); err != nil {
		return "", err
	}

	cleanKey, err := cleanAssetKey(assetKey)
	if err != nil {
		return "", err
	}

	return path.Join(s.baseDir, bucket, cleanKey), nil
}

func validateAssetBucket(bucket string) error {
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
	segments := strings.Split(normalized, "/")
	for _, segment := range segments {
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

var copyBufPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 32*1024)
		return &buf // 存指针，避免 interface 装箱时复制 slice header
	},
}

// saveAndHash 创建目录、写文件，同时流式计算 MD5 和 SHA256，避免大文件 OOM
func saveAndHash(src io.Reader, destPath string) (*storage.PutResult, error) {
	if err := os.MkdirAll(path.Dir(destPath), consts.FilePermDir); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", path.Dir(destPath), err)
	}

	dst, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, consts.FilePermFile)
	if err != nil {
		return nil, fmt.Errorf("create file %s: %w", destPath, err)
	}
	defer dst.Close()

	md5Hasher := md5.New()
	sha256Hasher := sha256.New()

	mw := io.MultiWriter(dst, md5Hasher, sha256Hasher)
	bufp := copyBufPool.Get().(*[]byte)
	defer copyBufPool.Put(bufp)
	size, err := io.CopyBuffer(mw, src, *bufp)
	if err != nil {
		// 写入失败清理残留文件，避免脏数据
		_ = os.Remove(destPath)
		return nil, fmt.Errorf("write file %s: %w", destPath, err)
	}

	return &storage.PutResult{
		StoragePath: destPath,
		Etag:        fmt.Sprintf("%x", md5Hasher.Sum(nil)),
		Sha256:      fmt.Sprintf("%x", sha256Hasher.Sum(nil)),
		Size:        size,
	}, nil
}
