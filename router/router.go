package router

import (
	"context"
	"oss/adaptor"
	"oss/api/auth"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/route"
	"github.com/hertz-contrib/logger/accesslog"
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
	CorsHandler      auth.ICorsHandler
	TokenHandler     auth.ITokenHandler
	EventHandler     auth.IEventHandler
	VideoHandler     auth.IVideoHandler
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
		CorsHandler:      auth.NewCorsCtrl(adaptor),
		TokenHandler:     auth.NewTokenCtrl(adaptor),
		EventHandler:     auth.NewEventCtrl(adaptor),
		VideoHandler:     auth.NewVideoCtrl(adaptor),
	}
}

func RegisterRoutes(h *server.Hertz, deps RouterDeps, adaptor adaptor.IAdaptor) {
	if adaptor.GetConfig().Server.Env == "dev" {
		h.Use(accesslog.New(
			accesslog.WithFormat("[${time}] ${status} ${latency} ${ip} ${method} ${path}\n"),
		))
		h.StaticFile("/debug/video", "./web/video-test-client/index.html")
	}

	if adaptor.GetConfig().Server.EnablePprof {
		pprof.Register(h)
	}

	registerPublicRoutes(h, deps)

	playGroup := h.Group("/api/v1",
		NewVideoPlayTokenMiddleware(adaptor),
		newVideoPlaybackCORSMiddleware(adaptor),
	)
	playGroup.OPTIONS("/video/*cors_path", func(ctx context.Context, c *app.RequestContext) {
		c.AbortWithStatus(204)
	})
	registerVideoPlaybackRoutes(playGroup, deps)

	authGroup := h.Group(
		"/api/v1",
		NewAccessKeyMiddleware(adaptor),
		newAuthenticatedCORSMiddleware(adaptor),
		NewOperationLogMiddleware(adaptor),
	)
	authGroup.OPTIONS("/*cors_path", func(ctx context.Context, c *app.RequestContext) {
		c.AbortWithStatus(204)
	})

	registerAuthRoutes(authGroup, deps)
	registerBucketRoutes(authGroup, deps, adaptor)
	registerObjectRoutes(authGroup, deps, adaptor)
	registerAdminRoutes(authGroup, deps)
}

func registerVideoPlaybackRoutes(playGroup *route.RouterGroup, deps RouterDeps) {
	videoGroup := playGroup.Group("/video")
	videoGroup.GET("/hls/:transcode_id/master.m3u8", deps.VideoHandler.GetHLSMasterPlaylist)
	videoGroup.GET("/hls/:transcode_id/:profile/index.m3u8", deps.VideoHandler.GetHLSProfilePlaylist)
	videoGroup.GET("/hls/:transcode_id/:profile/:segment", deps.VideoHandler.GetHLSSegment)
	videoGroup.GET("/keys/:key_id", deps.VideoHandler.GetHLSKey)
}

func registerPublicRoutes(h *server.Hertz, deps RouterDeps) {
	h.POST("/api/v1/access-keys", deps.AccessKeyHandler.CreateAccessKey)
	h.GET("/api/v1/access-keys", deps.AccessKeyHandler.ListAccessKeys)
	h.GET("/api/v1/access-keys/:access_key", deps.AccessKeyHandler.GetAccessKey)
	h.PATCH("/api/v1/access-keys/:access_key/status", deps.AccessKeyHandler.DeactivateAccessKey)
}

func registerAuthRoutes(authGroup *route.RouterGroup, deps RouterDeps) {
	authGroup.POST("/upload/tokens", deps.TokenHandler.CreateUploadToken)
	authGroup.POST("/download/tokens", deps.TokenHandler.CreateDownloadToken)

	// Management API: AK/SK + policy/ACL boundary.
	authGroup.POST("/video/play-tokens", deps.VideoHandler.CreatePlayToken)
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

	bucketGroup.POST("/buckets/:bucket_name/cors", deps.CorsHandler.CreateBucketCorsRule)
	bucketGroup.GET("/buckets/:bucket_name/cors", deps.CorsHandler.ListBucketCorsRules)
	bucketGroup.PUT("/buckets/:bucket_name/cors/:rule_id", deps.CorsHandler.UpdateBucketCorsRule)
	bucketGroup.DELETE("/buckets/:bucket_name/cors/:rule_id", deps.CorsHandler.DeleteBucketCorsRule)

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

	// Management API: AK/SK + policy/ACL boundary.
	objectGroup.GET("/buckets/:bucket_name/objects/:object_key/transcode", deps.VideoHandler.GetTranscodeStatus)

	objectGroup.POST("/buckets/:bucket_name/objects/:object_key/versions/:version_id/restore", deps.ObjectHandler.RestoreObjectVersion)
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
