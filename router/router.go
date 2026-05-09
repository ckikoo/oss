package router

import (
	"oss/adaptor"
	"oss/api/auth"
	"oss/service/lifecycle"
	"oss/service/policy"

	"github.com/cloudwego/hertz/pkg/app/server"
)

func RegisterRoutes(h *server.Hertz, adaptor adaptor.IAdaptor) {
	akCtrl := auth.NewCtrl(adaptor)
	bucketCtrl := auth.NewBucketCtrl(adaptor)

	objectCtrl := auth.NewObjectCtrl(adaptor)

	auditCtrl := auth.NewAuditCtrl(adaptor)

	meteringCtrl := auth.NewMeteringCtrl(adaptor)

	multipartCtrl := auth.NewMultipartCtrl(adaptor)
	policyService := policy.NewService(adaptor)
	policyCtrl := auth.NewPolicyCtrl(policyService)

	lifecycleService := lifecycle.NewService(adaptor)
	lifecycleCtrl := auth.NewLifecycleCtrl(lifecycleService)

	tokenCtrl := auth.NewTokenCtrl(adaptor)

	// TODO 后续完善管理端那边的
	h.POST("/api/v1/access-keys", akCtrl.CreateAccessKey)
	h.GET("/api/v1/access-keys", akCtrl.ListAccessKeys)
	h.GET("/api/v1/access-keys/:access_key", akCtrl.GetAccessKey)
	h.PATCH("/api/v1/access-keys/:access_key/status", akCtrl.DeactivateAccessKey)

	authGroup := h.Group("/api/v1", NewAccessKeyMiddleware(adaptor), NewOperationLogMiddleware(adaptor))

	authGroup.POST("/upload/tokens", tokenCtrl.CreateUploadToken)
	authGroup.POST("/download/tokens", tokenCtrl.CreateDownloadToken)

	// Bucket operations - require bucket ownership
	bucketGroup := authGroup.Group("", NewBucketACLMiddleware(adaptor))
	bucketGroup.POST("/buckets", bucketCtrl.CreateBucket)
	bucketGroup.GET("/buckets", bucketCtrl.ListBuckets)
	bucketGroup.GET("/buckets/:bucket_name", bucketCtrl.GetBucket)
	bucketGroup.PATCH("/buckets/:bucket_name", bucketCtrl.UpdateBucket)
	bucketGroup.DELETE("/buckets/:bucket_name", bucketCtrl.DeleteBucket)
	bucketGroup.POST("/buckets/:bucket_name/policies", policyCtrl.CreateBucketPolicy)
	bucketGroup.GET("/buckets/:bucket_name/policies", policyCtrl.ListBucketPolicies)

	bucketGroup.POST("/buckets/:bucket_name/lifecycle", lifecycleCtrl.CreateLifecycleRule)
	bucketGroup.GET("/buckets/:bucket_name/lifecycle", lifecycleCtrl.ListLifecycleRules)
	bucketGroup.GET("/buckets/:bucket_name/lifecycle/:rule_id", lifecycleCtrl.GetLifecycleRule)
	bucketGroup.PUT("/buckets/:bucket_name/lifecycle/:rule_id", lifecycleCtrl.UpdateLifecycleRule)
	bucketGroup.DELETE("/buckets/:bucket_name/lifecycle/:rule_id", lifecycleCtrl.DeleteLifecycleRule)

	// Object operations - check bucket ACL
	objectGroup := authGroup.Group("", NewObjectACLMiddleware(adaptor))
	objectGroup.GET("/buckets/:bucket_name/objects", objectCtrl.ListObjects)
	objectGroup.GET("/buckets/:bucket_name/objects/:object_key/metadata", objectCtrl.GetObjectMetadata)
	objectGroup.PUT("/buckets/:bucket_name/objects/:object_key", objectCtrl.PutObject)
	objectGroup.GET("/buckets/:bucket_name/objects/:object_key", objectCtrl.GetObject)
	objectGroup.DELETE("/buckets/:bucket_name/objects/:object_key", objectCtrl.DeleteObject)

	objectGroup.POST("/buckets/:bucket_name/multipart/uploads", multipartCtrl.CreateMultipartUpload)
	objectGroup.PUT("/buckets/:bucket_name/multipart/uploads/:upload_id/parts/:part_number", multipartCtrl.UploadMultipartPart)
	objectGroup.POST("/buckets/:bucket_name/multipart/uploads/:upload_id/complete", multipartCtrl.CompleteMultipartUpload)
	objectGroup.DELETE("/buckets/:bucket_name/multipart/uploads/:upload_id", multipartCtrl.AbortMultipartUpload)

	authGroup.GET("/metrics/daily", meteringCtrl.GetDailyMetering)
	authGroup.GET("/logs", auditCtrl.ListOperationLogs)

}
