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
	IVideoAssetStorage

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

// IVideoAssetStorage 视频派生资产存储接口。
// HLS m3u8/ts 等派生文件不进入普通 objects 表，也不通过 GetObject 暴露。
type IVideoAssetStorage interface {
	// PutAsset 保存派生资产，assetKey 由业务层生成，如 _video/{transcode_id}/{profile}/index.m3u8
	PutAsset(ctx context.Context, bucket string, assetKey string, src io.Reader) (*PutResult, error)

	// GetAsset 读取派生资产，调用方负责关闭返回的 ReadCloser
	GetAsset(ctx context.Context, bucket string, assetKey string) (io.ReadCloser, error)

	// DeleteAsset 删除单个派生资产，文件不存在时不报错
	DeleteAsset(ctx context.Context, bucket string, assetKey string) error

	// DeleteAssetPrefix 删除指定派生资产前缀，用于转码失败、对象删除和版本清理
	DeleteAssetPrefix(ctx context.Context, bucket string, prefix string) error

	// MoveAssetPrefix 将派生资产前缀整体移动到新前缀，用于 staging 发布
	MoveAssetPrefix(ctx context.Context, bucket string, srcPrefix string, dstPrefix string) error
}
