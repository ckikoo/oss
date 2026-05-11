package local

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"oss/adaptor/storage"
	"oss/consts"
)

// LocalStorage 本地磁盘存储实现
// 目录结构：
//
//	{baseDir}/{bucket}/{objectKey}                               ← 普通对象
//	{baseDir}/{bucket}/multipart/{uploadID}/part_{partNumber}   ← 分片
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
	dirPath := filepath.Join(s.baseDir, bucket, "multipart", uploadID)
	err := os.RemoveAll(dirPath)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// MergeParts：用 MultiReader 把分片串成单流，复用 saveAndHash
func (s *LocalStorage) MergeParts(ctx context.Context, bucket, objectKey string, partPaths []string) (*storage.PutResult, error) {
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
	return saveAndHash(io.MultiReader(files...), s.BuildObjectPath(ctx, bucket, objectKey, ""))
}

// BuildObjectPath 返回普通对象的完整磁盘路径（供外部记录到 DB）
func (s *LocalStorage) BuildObjectPath(ctx context.Context, bucket, objectKey string, version string) string {
	if version == "" {
		return filepath.Join(s.baseDir, bucket, objectKey)
	}

	return filepath.Join(s.baseDir, bucket, objectKey+"_"+version)
}

// ---- 内部辅助 ----

func (s *LocalStorage) buildPartPath(bucket, uploadID string, partNumber int32) string {
	return filepath.Join(s.baseDir, bucket, "multipart", uploadID, fmt.Sprintf("part_%d", partNumber))
}

var copyBufPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 32*1024)
		return &buf // 存指针，避免 interface 装箱时复制 slice header
	},
}

// saveAndHash 创建目录、写文件，同时流式计算 MD5 和 SHA256，避免大文件 OOM
func saveAndHash(src io.Reader, destPath string) (*storage.PutResult, error) {
	if err := os.MkdirAll(filepath.Dir(destPath), consts.FilePermDir); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", filepath.Dir(destPath), err)
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
