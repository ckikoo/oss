package storage

import (
	"fmt"
	"strings"
)

// UploadID 解析后的分片上传标识
type UploadID struct {
	Bucket    string
	ObjectKey string
	Version   string // 可为空；非空时为 32 位 hex（UUIDHex）
	UploadID  string // 本次上传的唯一标识（UUIDHex）
}

// FormatUploadID 将分片上传信息编码为 storageUploadID 字符串。
// 格式：{bucket}/{objectKey}_{version}/{id}
// 若 version 为空：{bucket}/{objectKey}/{id}
func FormatUploadID(bucket, objectKey, version, UploadID string) string {
	keyPart := objectKey
	if version != "" {
		keyPart = objectKey + "_" + version
	}
	return bucket + "/" + keyPart + "/" + UploadID
}

// ParseUploadID 解析 storageUploadID，返回结构化的 UploadID。
// version 固定为 32 位小写 hex（UUIDHex），用末尾 _ 分隔，解析无歧义。
func ParseUploadID(storageUploadID string) (UploadID, error) {
	// 第一个 / 前为 bucket，最后一个 / 后为 id，中间为 keyVersion
	first := strings.IndexByte(storageUploadID, '/')
	last := strings.LastIndexByte(storageUploadID, '/')
	if first < 0 || first == last {
		return UploadID{}, fmt.Errorf("invalid upload id: %s", storageUploadID)
	}

	bucket := storageUploadID[:first]
	keyVersion := storageUploadID[first+1 : last]
	id := storageUploadID[last+1:]

	if bucket == "" || keyVersion == "" || id == "" {
		return UploadID{}, fmt.Errorf("invalid upload id: %s", storageUploadID)
	}

	// 从 keyVersion 末尾拆出 version（32 位 hex）
	objectKey := keyVersion
	version := ""
	if idx := strings.LastIndexByte(keyVersion, '_'); idx >= 0 {
		if tail := keyVersion[idx+1:]; IsHex32(tail) {
			objectKey = keyVersion[:idx]
			version = tail
		}
	}

	return UploadID{
		Bucket:    bucket,
		ObjectKey: objectKey,
		Version:   version,
		UploadID:  id,
	}, nil
}

// IsHex32 检查字符串是否为 32 位小写十六进制（UUIDHex 格式）。
func IsHex32(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
