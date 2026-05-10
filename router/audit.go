package router

import (
	"context"
	"time"

	"oss/adaptor"
	"oss/adaptor/repo/audit"
	"oss/common"
	"oss/consts"
	"oss/service/do"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/google/uuid"
)

// NewOperationLogMiddleware creates a middleware that logs all operations
func NewOperationLogMiddleware(adaptor adaptor.IAdaptor) app.HandlerFunc {
	auditRepo := audit.NewOperationLogRepo(adaptor.GetGORM())
	return func(ctx context.Context, c *app.RequestContext) {
		requestID := uuid.NewString()
		c.Set("request_id", requestID)

		startTime := time.Now()
		requestSize := int64(len(c.GetRawData()))

		// Continue to next handler
		c.Next(ctx)

		// After response is written, log the operation
		duration := time.Since(startTime)
		durationMs := int32(duration.Milliseconds())

		// Extract user info if available
		var userID *int64
		var accessKey *string
		if userInfo, ok := c.Get(consts.UserInfoContext); ok {
			if info, ok := userInfo.(*common.UserInfoCtx); ok {
				userID = &info.UserID
				accessKey = &info.AccessKey
			}
		}

		// Extract request/response info
		clientIP := c.ClientIP()
		userAgent := string(c.UserAgent())
		statusCode := c.Response.StatusCode()
		bucketName := c.Param("bucket_name")
		objectKey := c.Param("object_key")

		// Determine action from method and path
		method := string(c.Method())
		path := string(c.Path())
		action := method + " " + path

		// Determine result: 0 = success, 1 = failed
		result := int32(0)
		if statusCode >= 400 {
			result = 1
		}

		// Build operation log
		opLog := &do.CreateOperationLog{
			RequestID:    requestID,
			UserID:       userID,
			AccessKey:    accessKey,
			Action:       action,
			Result:       result,
			StatusCode:   int32(statusCode),
			ClientIP:     &clientIP,
			UserAgent:    &userAgent,
			RequestSize:  requestSize,
			ResponseSize: int64(c.Response.Header.ContentLength()),
			DurationMs:   durationMs,
		}

		// Set bucket and object info if present
		if bucketName != "" {
			opLog.BucketName = &bucketName
		}
		if objectKey != "" {
			opLog.ObjectKey = &objectKey
		}

		// Log the operation asynchronously to avoid blocking response
		go func() {
			_ = auditRepo.CreateOperationLog(context.Background(), opLog)
		}()
	}
}
