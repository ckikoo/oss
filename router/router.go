package router

import (
	"oss/adaptor"
	"oss/api/auth"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/hertz-contrib/pprof"
)

type RouterDeps struct {
	AccessKeyHandler auth.IAccessKeyHandler
	BucketHandler    auth.IBucketHandler
	ObjectHandler    auth.IObjectHandler
	AuditHandler     auth.IAuditHandler
	MeteringHandler  auth.IMeteringHandler
	MultipartHandler auth.IMultipartHandler
	PolicyHandler    auth.IPolicyHandler
	LifecycleHandler auth.ILifecycleHandler
	TokenHandler     auth.ITokenHandler
	EventHandler     auth.IEventHandler
}

func NewRouterDeps(adaptor adaptor.IAdaptor) RouterDeps {
	return RouterDeps{
		AccessKeyHandler: auth.NewCtrl(adaptor),
		BucketHandler:    auth.NewBucketCtrl(adaptor),
		ObjectHandler:    auth.NewObjectCtrl(adaptor),
		AuditHandler:     auth.NewAuditCtrl(adaptor),
		MeteringHandler:  auth.NewMeteringCtrl(adaptor),
		MultipartHandler: auth.NewMultipartCtrl(adaptor),
		PolicyHandler:    auth.NewPolicyCtrl(adaptor),
		LifecycleHandler: auth.NewLifecycleCtrl(adaptor),
		TokenHandler:     auth.NewTokenCtrl(adaptor),
		EventHandler:     auth.NewEventCtrl(adaptor),
	}
}

func RegisterRoutes(h *server.Hertz, deps RouterDeps, adaptor adaptor.IAdaptor) {
	h.Use(globalCORSMiddleware())

	registerPublicRoutes(h, deps)
	authGroup := h.Group("/api/v1", NewAccessKeyMiddleware(adaptor), NewOperationLogMiddleware(adaptor))
	registerAuthRoutes(authGroup, deps)
	registerBucketRoutes(authGroup, deps, adaptor)
	registerObjectRoutes(authGroup, deps, adaptor)
	registerAdminRoutes(authGroup, deps)
}

func registerPublicRoutes(h *server.Hertz, deps RouterDeps) {
	// Access key management is public to the app, but still part of the OSS API surface.
	h.POST("/api/v1/access-keys", deps.AccessKeyHandler.CreateAccessKey)
	h.GET("/api/v1/access-keys", deps.AccessKeyHandler.ListAccessKeys)
	h.GET("/api/v1/access-keys/:access_key", deps.AccessKeyHandler.GetAccessKey)
	h.PATCH("/api/v1/access-keys/:access_key/status", deps.AccessKeyHandler.DeactivateAccessKey)

	pprof.Register(h)
}

func registerAuthRoutes(authGroup *route.RouterGroup, deps RouterDeps) {
	authGroup.POST("/upload/tokens", deps.TokenHandler.CreateUploadToken)
	authGroup.POST("/download/tokens", deps.TokenHandler.CreateDownloadToken)
}

func registerBucketRoutes(authGroup *route.RouterGroup, deps RouterDeps, adaptor adaptor.IAdaptor) {
	bucketGroup := authGroup.Group("", NewBucketACLMiddleware(adaptor))
	bucketGroup.POST("/buckets", deps.BucketHandler.CreateBucket)
	bucketGroup.GET("/buckets", deps.BucketHandler.ListBuckets)
	bucketGroup.GET("/buckets/:bucket_name", deps.BucketHandler.GetBucket)
	bucketGroup.PATCH("/buckets/:bucket_name", deps.BucketHandler.UpdateBucket)
	bucketGroup.DELETE("/buckets/:bucket_name", deps.BucketHandler.DeleteBucket)

	bucketGroup.POST("/buckets/:bucket_name/policies", deps.PolicyHandler.CreateBucketPolicy)
	bucketGroup.GET("/buckets/:bucket_name/policies", deps.PolicyHandler.ListBucketPolicies)

	bucketGroup.POST("/buckets/:bucket_name/lifecycle", deps.LifecycleHandler.CreateLifecycleRule)
	bucketGroup.GET("/buckets/:bucket_name/lifecycle", deps.LifecycleHandler.ListLifecycleRules)
	bucketGroup.GET("/buckets/:bucket_name/lifecycle/:rule_id", deps.LifecycleHandler.GetLifecycleRule)
	bucketGroup.PUT("/buckets/:bucket_name/lifecycle/:rule_id", deps.LifecycleHandler.UpdateLifecycleRule)
	bucketGroup.DELETE("/buckets/:bucket_name/lifecycle/:rule_id", deps.LifecycleHandler.DeleteLifecycleRule)

	bucketGroup.POST("/buckets/:bucket_name/events", deps.EventHandler.CreateEventRule)
	bucketGroup.GET("/buckets/:bucket_name/events", deps.EventHandler.ListEventRules)
	bucketGroup.PUT("/buckets/:bucket_name/events/:rule_id", deps.EventHandler.UpdateEventRule)
	bucketGroup.DELETE("/buckets/:bucket_name/events/:rule_id", deps.EventHandler.DeleteEventRule)
}

func registerObjectRoutes(authGroup *route.RouterGroup, deps RouterDeps, adaptor adaptor.IAdaptor) {
	objectGroup := authGroup.Group("", NewPolicyMiddleware(adaptor), NewObjectACLMiddleware(adaptor))
	objectGroup.GET("/buckets/:bucket_name/objects", deps.ObjectHandler.ListObjects)
	objectGroup.GET("/buckets/:bucket_name/objects/:object_key/metadata", deps.ObjectHandler.GetObjectMetadata)
	objectGroup.GET("/buckets/:bucket_name/objects/:object_key/versions", deps.ObjectHandler.GetObjectVersions)
	objectGroup.PUT("/buckets/:bucket_name/objects/:object_key", deps.ObjectHandler.PutObject)
	objectGroup.GET("/buckets/:bucket_name/objects/:object_key", deps.ObjectHandler.GetObject)
	objectGroup.DELETE("/buckets/:bucket_name/objects/:object_key", deps.ObjectHandler.DeleteObject)

	objectGroup.POST("/buckets/:bucket_name/multipart/uploads", deps.MultipartHandler.CreateMultipartUpload)
	objectGroup.PUT("/buckets/:bucket_name/multipart/uploads/:upload_id/parts/:part_number", deps.MultipartHandler.UploadMultipartPart)
	objectGroup.POST("/buckets/:bucket_name/multipart/uploads/:upload_id/complete", deps.MultipartHandler.CompleteMultipartUpload)
	objectGroup.DELETE("/buckets/:bucket_name/multipart/uploads/:upload_id", deps.MultipartHandler.AbortMultipartUpload)
}

func registerAdminRoutes(authGroup *route.RouterGroup, deps RouterDeps) {
	authGroup.GET("/metrics/daily", deps.MeteringHandler.GetDailyMetering)
	authGroup.GET("/logs", deps.AuditHandler.ListOperationLogs)
}
