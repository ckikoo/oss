package auth

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"oss/api"
	"oss/common"
	"oss/consts"
	"oss/service/dto"
	"oss/service/object"
	"path/filepath"

	"github.com/cloudwego/hertz/pkg/app"
)

type ObjectCtrl struct {
	object *object.Service
}

func NewObjectCtrl(service *object.Service) *ObjectCtrl {
	return &ObjectCtrl{object: service}
}

func (ctrl *ObjectCtrl) ListObjects(ctx context.Context, c *app.RequestContext) {
	req := &dto.ListObjectsReq{}
	if err := c.BindAndValidate(req); err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
		return
	}

	bucketName := c.Param("bucket_name")
	req.BucketName = bucketName

	resp, errno := ctrl.object.ListObjects(ctx, req)
	api.WriteResp(c, resp, errno)
}

func (ctrl *ObjectCtrl) GetObjectMetadata(ctx context.Context, c *app.RequestContext) {
	bucketName := c.Param("bucket_name")
	objectKey := c.Param("object_key")
	versionID := c.Query("version_id")

	if bucketName == "" || objectKey == "" {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("bucket_name and object_key are required"))
		return
	}

	resp, errno := ctrl.object.GetObjectMetadata(ctx, bucketName, objectKey, versionID)
	api.WriteResp(c, resp, errno)
}

func (ctrl *ObjectCtrl) PutObject(ctx context.Context, c *app.RequestContext) {
	bucketName := c.Param("bucket_name")
	objectKey := c.Param("object_key")

	if bucketName == "" || objectKey == "" {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("bucket_name and object_key are required"))
		return
	}

	// Get file from multipart form
	file, err := c.FormFile("file")
	if err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("file is required"))
		return
	}

	// Get other parameters
	contentType := c.PostForm("content_type")
	if contentType == "" {
		contentType = file.Header.Get("Content-Type")
	}
	storageClass := c.PostForm("storage_class")
	if storageClass == "" {
		storageClass = consts.StorageClassStandard
	}
	aclStr := c.PostForm("acl")
	acl := int32(consts.ObjectAclInheritBucket)
	if aclStr != "" {
		// Parse acl if needed
		// TODO: Implement ACL parsing
	}
	metadata := c.PostForm("metadata")

	req := &dto.PutObjectReq{
		BucketName:   bucketName,
		ObjectKey:    objectKey,
		ContentType:  contentType,
		StorageClass: storageClass,
		Acl:          acl,
		Metadata:     metadata,
	}

	resp, errno := ctrl.object.PutObject(ctx, req, file)
	api.WriteResp(c, resp, errno)
}

func (ctrl *ObjectCtrl) GetObject(ctx context.Context, c *app.RequestContext) {
	bucketName := c.Param("bucket_name")
	objectKey := c.Param("object_key")
	versionID := c.Query("version_id")

	if bucketName == "" || objectKey == "" {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("bucket_name and object_key are required"))
		return
	}

	errno := ctrl.object.GetObject(ctx, bucketName, objectKey, versionID, c)
	if errno.NotOk() {
		api.WriteResp(c, nil, errno)
	}
}

func (ctrl *ObjectCtrl) DeleteObject(ctx context.Context, c *app.RequestContext) {
	bucketName := c.Param("bucket_name")
	objectKey := c.Param("object_key")
	versionID := c.Query("version_id")

	if bucketName == "" || objectKey == "" {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("bucket_name and object_key are required"))
		return
	}

	errno := ctrl.object.DeleteObject(ctx, bucketName, objectKey, versionID)
	api.WriteResp(c, nil, errno)
}
func saveFileAndComputeHashes(src io.Reader, destPath string) (etag string, sha256sum string, size int64, err error) {
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
