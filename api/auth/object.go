package auth

import (
	"context"
	"oss/adaptor"
	"oss/api"
	"oss/common"
	"oss/consts"
	"oss/service/dto"
	"oss/service/object"

	"github.com/cloudwego/hertz/pkg/app"
)

type ObjectCtrl struct {
	object *object.Service
}

var _ IObjectHandler = (*ObjectCtrl)(nil)

func NewObjectCtrl(adaptor adaptor.IAdaptor) IObjectHandler {
	return &ObjectCtrl{object: object.NewService(adaptor)}
}

func (ctrl *ObjectCtrl) ListObjects(ctx context.Context, c *app.RequestContext) {
	ctx1, pass := common.GetUserInfoFromContext(ctx, c)
	if !pass {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

	req := &dto.ListObjectsReq{}
	if err := c.BindAndValidate(req); err != nil {
		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
		return
	}

	bucketName := c.Param("bucket_name")
	req.BucketName = bucketName

	resp, errno := ctrl.object.ListObjects(ctx1, req)
	api.WriteResp(c, resp, errno)
}

func (ctrl *ObjectCtrl) GetObjectMetadata(ctx context.Context, c *app.RequestContext) {
	ctx1, pass := common.GetUserInfoFromContext(ctx, c)
	if !pass {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

	bucketName := c.Param("bucket_name")
	objectKey := c.Param("object_key")
	versionID := c.Query("version_id")

	if bucketName == "" || objectKey == "" {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("bucket_name and object_key are required"))
		return
	}

	resp, errno := ctrl.object.GetObjectMetadata(ctx1, bucketName, objectKey, versionID)
	api.WriteResp(c, resp, errno)
}

func (ctrl *ObjectCtrl) GetObjectVersions(ctx context.Context, c *app.RequestContext) {
	ctx1, pass := common.GetUserInfoFromContext(ctx, c)
	if !pass {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

	bucketName := c.Param("bucket_name")
	objectKey := c.Param("object_key")

	if bucketName == "" || objectKey == "" {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("bucket_name and object_key are required"))
		return
	}

	resp, errno := ctrl.object.GetObjectVersions(ctx1, bucketName, objectKey)
	api.WriteResp(c, resp, errno)
}

func (ctrl *ObjectCtrl) PutObject(ctx context.Context, c *app.RequestContext) {
	ctx1, pass := common.GetUserInfoFromContext(ctx, c)
	if !pass {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

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
		switch aclStr {
		case "private":
			acl = consts.ObjectAclPrivate
		case "public-read":
			acl = consts.ObjectAclPublicRead
		case "default", "":
			acl = consts.ObjectAclInheritBucket
		default:
			api.WriteResp(c, nil, common.ParamErr.WithMsg("invalid acl value"))
			return
		}
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

	resp, errno := ctrl.object.PutObject(ctx1, req, file)
	api.WriteResp(c, resp, errno)
}

func (ctrl *ObjectCtrl) GetObject(ctx context.Context, c *app.RequestContext) {
	ctx1, pass := common.GetUserInfoFromContext(ctx, c)
	if !pass {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

	bucketName := c.Param("bucket_name")
	objectKey := c.Param("object_key")
	versionID := c.Query("version_id")

	if bucketName == "" || objectKey == "" {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("bucket_name and object_key are required"))
		return
	}

	errno := ctrl.object.GetObject(ctx1, bucketName, objectKey, versionID, c)
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

	ctx1, pass := common.GetUserInfoFromContext(ctx, c)
	if !pass {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

	errno := ctrl.object.DeleteObject(ctx1, bucketName, objectKey, versionID)
	api.WriteResp(c, nil, errno)
}

func (ctrl *ObjectCtrl) RestoreObjectVersion(ctx context.Context, c *app.RequestContext) {
	ctx1, pass := common.GetUserInfoFromContext(ctx, c)
	if !pass {
		api.WriteResp(c, nil, common.AuthErr)
		return
	}

	bucketName := c.Param("bucket_name")
	objectKey := c.Param("object_key")
	versionID := c.Param("version_id")

	if bucketName == "" || objectKey == "" || versionID == "" {
		api.WriteResp(c, nil, common.ParamErr.WithMsg("bucket_name, object_key and version_id are required"))
		return
	}

	req := &dto.RestoreObjectVersionReq{}
	if len(c.Request.Body()) > 0 {
		if err := c.BindAndValidate(req); err != nil {
			api.WriteResp(c, nil, common.ParamErr.WithErr(err))
			return
		}
	}

	resp, errno := ctrl.object.RestoreObjectVersion(ctx1, bucketName, objectKey, versionID, req)
	api.WriteResp(c, resp, errno)
}
