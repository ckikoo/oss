package tools

import (
	"crypto/md5"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"oss/consts"
	"path/filepath"
)

// SaveFileAndComputeHashes 计算文件的SHA256哈希值，支持流式处理避免大文件OOM
func SaveFileAndComputeHashes(src io.Reader, destPath string) (etag string, sha256sum string, size int64, err error) {
	if err = os.MkdirAll(filepath.Dir(destPath), consts.FilePermDir); err != nil {
		return
	}

	dst, err := os.Create(destPath)
	if err != nil {
		return
	}
	defer dst.Close()

	md5Hasher := md5.New()
	sha256Hasher := sha256.New()

	// 一次读取，同时写盘 + 算两个 hash
	mw := io.MultiWriter(dst, md5Hasher, sha256Hasher)
	size, err = io.Copy(mw, src)
	if err != nil {
		return
	}

	etag = fmt.Sprintf("%x", md5Hasher.Sum(nil))
	sha256sum = fmt.Sprintf("%x", sha256Hasher.Sum(nil))
	return
}
