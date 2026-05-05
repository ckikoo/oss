package router

import (
	"oss/adaptor"
	"oss/api/auth"
	"oss/service/accesskey"
	"oss/service/audit"
	"oss/service/bucket"
	"oss/service/lifecycle"
	"oss/service/metering"
	"oss/service/mutipart"
	"oss/service/object"
	"oss/service/policy"

	"github.com/cloudwego/hertz/pkg/app/server"
)

func RegisterRoutes(h *server.Hertz, adaptor adaptor.IAdaptor) {
	akService := accesskey.NewService(adaptor)
	akCtrl := auth.NewCtrl(akService)
	bucketService := bucket.NewService(adaptor)
	bucketCtrl := auth.NewBucketCtrl(bucketService)
	objectService := object.NewService(adaptor)
	objectCtrl := auth.NewObjectCtrl(objectService)
	auditService := audit.NewService(adaptor)
	auditCtrl := auth.NewAuditCtrl(auditService)

	meteringService := metering.NewService(adaptor)
	meteringCtrl := auth.NewMeteringCtrl(meteringService)

	mutipartService := mutipart.NewService(adaptor)
	mutipartCtrl := auth.NewMutipartCtrl(mutipartService)
	policyService := policy.NewService(adaptor)
	policyCtrl := auth.NewPolicyCtrl(policyService)

	lifecycleService := lifecycle.NewService(adaptor)
	lifecycleCtrl := auth.NewLifecycleCtrl(lifecycleService)

	tokenCtrl := auth.NewTokenCtrl(adaptor)

	h.POST("/api/v1/access-keys", akCtrl.CreateAccessKey)
	h.GET("/api/v1/access-keys", akCtrl.ListAccessKeys)
	h.GET("/api/v1/access-keys/:access_key", akCtrl.GetAccessKey)
	h.PATCH("/api/v1/access-keys/:access_key/status", akCtrl.DeactivateAccessKey)

	authGroup := h.Group("/api/v1", NewAccessKeyMiddleware(adaptor))

	authGroup.POST("/upload/tokens", tokenCtrl.CreateUploadToken)
	authGroup.POST("/download/tokens", tokenCtrl.CreateDownloadToken)

	authGroup.POST("/buckets", bucketCtrl.CreateBucket)
	authGroup.GET("/buckets", bucketCtrl.ListBuckets)
	authGroup.GET("/buckets/:bucket_name", bucketCtrl.GetBucket)
	authGroup.PATCH("/buckets/:bucket_name", bucketCtrl.UpdateBucket)
	authGroup.DELETE("/buckets/:bucket_name", bucketCtrl.DeleteBucket)
	authGroup.POST("/buckets/:bucket_name/policies", policyCtrl.CreateBucketPolicy)
	authGroup.GET("/buckets/:bucket_name/policies", policyCtrl.ListBucketPolicies)

	authGroup.POST("/buckets/:bucket_name/lifecycle", lifecycleCtrl.CreateLifecycleRule)
	authGroup.GET("/buckets/:bucket_name/lifecycle", lifecycleCtrl.ListLifecycleRules)
	authGroup.GET("/buckets/:bucket_name/lifecycle/:rule_id", lifecycleCtrl.GetLifecycleRule)
	authGroup.PUT("/buckets/:bucket_name/lifecycle/:rule_id", lifecycleCtrl.UpdateLifecycleRule)
	authGroup.DELETE("/buckets/:bucket_name/lifecycle/:rule_id", lifecycleCtrl.DeleteLifecycleRule)

	authGroup.GET("/buckets/:bucket_name/objects", objectCtrl.ListObjects)
	authGroup.GET("/buckets/:bucket_name/objects/:object_key/metadata", objectCtrl.GetObjectMetadata)
	authGroup.PUT("/buckets/:bucket_name/objects/:object_key", objectCtrl.PutObject)
	authGroup.GET("/buckets/:bucket_name/objects/:object_key", objectCtrl.GetObject)
	authGroup.DELETE("/buckets/:bucket_name/objects/:object_key", objectCtrl.DeleteObject)
	authGroup.GET("/metrics/daily", meteringCtrl.GetDailyMetering)
	authGroup.GET("/logs", auditCtrl.ListOperationLogs)

	authGroup.POST("/buckets/:bucket_name/multipart/uploads", mutipartCtrl.CreateMultipartUpload)
	authGroup.PUT("/buckets/:bucket_name/multipart/uploads/:upload_id/parts/:part_number", mutipartCtrl.UploadMultipartPart)
	authGroup.POST("/buckets/:bucket_name/multipart/uploads/:upload_id/complete", mutipartCtrl.CompleteMultipartUpload)
	authGroup.DELETE("/buckets/:bucket_name/multipart/uploads/:upload_id", mutipartCtrl.AbortMultipartUpload)

}
