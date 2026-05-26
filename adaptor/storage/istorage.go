package storage

import (
	"context"
	"io"
)

// PutResult 写入文件后的结果
type PutResult struct {
	StoragePath string
	Etag        string // MD5 hex（原生分片合并后为复合 ETag，格式 hash-N）
	Size        int64
}

type StatResult struct {
	Exist bool
}

// PartInfo 分片信息，用于 CompleteMultipartUpload
type PartInfo struct {
	PartNumber int32
	ETag       string // PutPart 返回的 ETag，必须原样传回
}

// IStorage 文件存储抽象接口
type IStorage interface {
	IVideoAssetStorage

	// Put 保存普通对象（小文件直传），返回存储路径和哈希信息
	Put(ctx context.Context, bucket, objectKey string, version string, src io.Reader) (*PutResult, error)

	// Get 读取文件，调用方负责关闭返回的 ReadCloser
	Get(ctx context.Context, storagePath string) (io.ReadCloser, error)

	// Stat 检查对象是否存在
	Stat(ctx context.Context, storagePath string) (*StatResult, error)

	// Delete 删除单个文件，文件不存在时不报错
	Delete(ctx context.Context, storagePath string) error

	Copy(ctx context.Context, srcStoragePath string, dstBucket, dstKey, dstVersion string) (*PutResult, error)

	// --- 分片上传（大文件） ---

	// CreateMultipartUpload 初始化分片上传会话。
	// 返回 storageUploadID，编码了后端所需的全部定位信息（bucket/key/s3UploadId 等）。
	// 调用方应持久化 storageUploadID，用于后续 PutPart / Complete / Abort。
	CreateMultipartUpload(ctx context.Context, bucket, objectKey, version string) (storageUploadID string, err error)

	// PutPart 上传单个分片。
	// storageUploadID 来自 CreateMultipartUpload。
	// 返回的 PutResult.Etag 必须原样保存，CompleteMultipartUpload 时需要传回。
	PutPart(ctx context.Context, bucket, objectKey, version string, storageUploadID string, partNumber int32, src io.Reader) (*PutResult, error)

	// CompleteMultipartUpload 通知后端合并所有分片，完成上传。
	// parts 须按 PartNumber 升序排列，ETag 来自 PutPart 返回值。
	// 注意：返回的 PutResult.Sha256 为空，调用方应自行维护整体 SHA256。
	CompleteMultipartUpload(ctx context.Context, bucket, objectKey, version string, storageUploadID string, parts []PartInfo) (*PutResult, error)

	// AbortUpload 取消分片上传并清理已上传的所有分片。
	// 幂等：uploadID 不存在或已完成时不报错。
	AbortUpload(ctx context.Context, bucket, objectKey, version, storageID string) error
}

// IVideoAssetStorage 视频派生资产存储接口。
// HLS m3u8/ts 等派生文件不进入普通 objects 表，也不通过 Get 暴露。
type IVideoAssetStorage interface {
	// PutAsset 保存派生资产，assetKey 由业务层生成，如 _video/{transcode_id}/{profile}/index.m3u8
	PutAsset(ctx context.Context, bucket string, assetKey string, src io.Reader) (*PutResult, error)

	// GetAsset 读取派生资产，调用方负责关闭返回的 ReadCloser
	GetAsset(ctx context.Context, bucket string, assetKey string) (io.ReadCloser, error)

	// DeleteAsset 删除单个派生资产，文件不存在时不报错
	DeleteAsset(ctx context.Context, bucket string, assetKey string) error

	// DeleteAssetPrefix 删除指定前缀下的全部派生资产，用于转码失败、对象删除和版本清理
	DeleteAssetPrefix(ctx context.Context, bucket string, prefix string) error

	// MoveAssetPrefix 将派生资产前缀整体移动到新前缀，用于 staging 发布
	MoveAssetPrefix(ctx context.Context, bucket string, srcPrefix string, dstPrefix string) error
}
