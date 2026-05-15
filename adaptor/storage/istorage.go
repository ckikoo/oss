package storage

import (
	"context"
	"io"
)

// PutResult 写入文件后的结果
type PutResult struct {
	StoragePath string
	Etag        string // MD5 hex
	Sha256      string // SHA256 hex
	Size        int64
}

// IStorage 文件存储抽象接口
// 目前实现：local（本地磁盘）
// 未来可扩展：S3、MinIO、OSS 等，service 层无需改动
type IStorage interface {
	// Put 保存普通对象，返回存储路径和哈希信息
	Put(ctx context.Context, bucket, objectKey string, version string, src io.Reader) (*PutResult, error)

	// Get 读取文件，调用方负责关闭返回的 ReadCloser
	Get(ctx context.Context, storagePath string) (io.ReadCloser, error)

	// Delete 删除单个文件，文件不存在时不报错
	Delete(ctx context.Context, storagePath string) error

	// PutPart 保存分片，路径规则由实现层维护
	// 返回的 StoragePath 会被记录到 multipart_parts 表
	PutPart(ctx context.Context, bucket, uploadID string, partNumber int32, src io.Reader) (*PutResult, error)

	// DeletePart 删除某次分片上传的单个分片目录
	DeletePart(ctx context.Context, bucket, uploadID string, partNum int32) error

	// DeleteParts 删除某次分片上传的全部分片目录
	DeleteParts(ctx context.Context, bucket, uploadID string) error

	// MergeParts 将已保存的分片按顺序合并成一个完整对象文件
	MergeParts(ctx context.Context, bucket, objectKey string, version string, partPaths []string) (*PutResult, error)

	// BuildObjectPath 给外部（如 CompleteMultipart）查询对象最终路径用
	BuildObjectPath(ctx context.Context, bucket, objectKey string, version string) string
}
