package router

import (
	"context"
	"oss/adaptor"
	gormBucket "oss/adaptor/repo/bucket/gorm"
	gormObject "oss/adaptor/repo/object/gorm"
	"oss/common"
	"oss/consts"

	"github.com/cloudwego/hertz/pkg/app"
)

// NewBucketACLMiddleware checks bucket ACL for operations
func NewBucketACLMiddleware(adaptor adaptor.IAdaptor) app.HandlerFunc {
	bucketRepo := gormBucket.NewBucketRepo(adaptor)
	return func(ctx context.Context, c *app.RequestContext) {
		userID := authenticatedUserID(c)
		if userID == 0 {
			c.JSON(401, common.AuthErr.WithMsg("user not authenticated"))
			c.Abort()
			return
		}

		bucketName := c.Param("bucket_name")
		if bucketName == "" {
			c.Next(ctx)
			return
		}

		bucketDo, err := loadRouteBucket(ctx, c, bucketRepo, userID, bucketName)
		if err != nil {
			c.JSON(404, common.ParamErr.WithMsg("bucket not found"))
			c.Abort()
			return
		}

		if bucketDo == nil {
			c.JSON(404, common.ResouceNotFoundErr)
			c.Abort()
			return
		}

		// Set bucket info in context for later use
		c.Set(consts.BucketContext, bucketDo)

		c.Next(ctx)
	}
}

// NewObjectACLMiddleware checks object ACL based on bucket ACL and object ACL
func NewObjectACLMiddleware(adaptor adaptor.IAdaptor) app.HandlerFunc {
	bucketRepo := gormBucket.NewBucketRepo(adaptor)
	objectRepo := gormObject.NewObjectRepo(adaptor)
	return func(ctx context.Context, c *app.RequestContext) {
		userID := authenticatedUserID(c)
		if userID == 0 {
			c.JSON(401, common.AuthErr.WithMsg("user not authenticated"))
			c.Abort()
			return
		}

		bucketName := c.Param("bucket_name")
		if bucketName == "" {
			c.Next(ctx)
			return
		}

		bucketDo, err := loadRouteBucket(ctx, c, bucketRepo, userID, bucketName)
		if err != nil {
			c.JSON(404, common.ParamErr.WithMsg("bucket not found"))
			c.Abort()
			return
		}

		method := string(c.Method())
		isWrite := method == "PUT" || method == "POST" || method == "DELETE"

		// Determine effective ACL
		effectiveAcl := bucketDo.Acl
		isFromObject := false

		objectKey := c.Param("object_key")
		if objectKey != "" {
			objectDo, err := loadRouteObject(ctx, c, objectRepo, bucketDo.Name, objectKey, "")
			if err == nil && objectDo != nil { // Object exists
				if objectDo.Acl != consts.ObjectAclInheritBucket {
					effectiveAcl = objectDo.Acl
					isFromObject = true
				}
			} else if isWrite {
				// For write operations on non-existing objects, use bucket ACL
				// Allow if user can write to bucket
			} else {
				// For read operations on non-existing objects, deny
				c.JSON(404, common.ParamErr.WithMsg("object not found"))
				c.Abort()
				return
			}
		}

		// Check ACL
		isPrivate := false
		isPublicRead := false
		isPublicRW := false

		if isFromObject {
			switch effectiveAcl {
			case consts.ObjectAclPrivate:
				isPrivate = true
			case consts.ObjectAclPublicRead:
				isPublicRead = true
			}
		} else {
			switch effectiveAcl {
			case consts.BucketAclPrivate:
				isPrivate = true
			case consts.BucketAclPublicRead:
				isPublicRead = true
			case consts.BucketAclPublicRW:
				isPublicRW = true
			}
		}

		if isPrivate {
			if bucketDo.UserID != userID {
				c.JSON(403, common.AuthErr.WithMsg("access denied: private"))
				c.Abort()
				return
			}
		} else if isPublicRead {
			if isWrite && bucketDo.UserID != userID {
				c.JSON(403, common.AuthErr.WithMsg("access denied: read only"))
				c.Abort()
				return
			}

		} else if !isPublicRW {
			c.JSON(403, common.AuthErr.WithMsg("invalid ACL"))
			c.Abort()
			return
		}

		// Set bucket info in context
		c.Set(consts.BucketContext, bucketDo)

		c.Next(ctx)
	}
}
