package common

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
)

const S3XMLContentType = "application/xml"

type S3Error struct {
	XMLName   xml.Name `xml:"Error"`
	Code      string   `xml:"Code"`
	Message   string   `xml:"Message"`
	Resource  string   `xml:"Resource,omitempty"`
	RequestID string   `xml:"RequestId"`
}

func WriteS3XML(c *app.RequestContext, status int, data any) {
	body, err := xml.Marshal(data)
	if err != nil {
		WriteS3Error(c, ServerErr, string(c.Path()))
		return
	}

	out := append([]byte(xml.Header), body...)
	c.Data(status, S3XMLContentType, out)
}

func WriteS3Empty(c *app.RequestContext, status int) {
	c.Data(status, S3XMLContentType, nil)
}

func WriteS3Error(c *app.RequestContext, errno Errno, resource string) {
	status, code, msg := MapS3Error(errno)
	WriteS3ErrorCode(c, status, code, msg, resource)
}

func WriteS3ErrorCode(c *app.RequestContext, status int, code, msg, resource string) {
	if resource == "" {
		resource = string(c.Path())
	}
	reqID := string(c.GetHeader("X-Request-Id"))
	if reqID == "" {
		reqID = fmt.Sprintf("%d", time.Now().UnixNano())
	}

	WriteS3XML(c, status, S3Error{
		Code:      code,
		Message:   msg,
		Resource:  resource,
		RequestID: reqID,
	})
}

func MapS3Error(errno Errno) (int, string, string) {
	switch errno.Code {
	case OK.Code:
		return http.StatusOK, "OK", "OK"
	case AuthErr.Code, PermissionErr.Code:
		return http.StatusForbidden, "AccessDenied", "Access Denied"
	case BucketNotFoundErr.Code:
		return http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist"
	case ResouceNotFoundErr.Code:
		return http.StatusNotFound, "NoSuchKey", "The specified key does not exist"
	case ParamErr.Code:
		return http.StatusBadRequest, "InvalidArgument", "Invalid Argument"
	case FileNameExists.Code:
		return http.StatusPreconditionFailed, "PreconditionFailed", "At least one of the preconditions you specified did not hold"
	case FileUploadIdNotFound.Code:
		return http.StatusNotFound, "NoSuchUpload", "The specified multipart upload does not exist"
	case FilePartSizeOutLimit.Code:
		return http.StatusBadRequest, "EntityTooLarge", "Your proposed upload exceeds the maximum allowed size"
	case DatabaseErr.Code, ServerErr.Code:
		return http.StatusInternalServerError, "InternalError", "We encountered an internal error"
	default:
		if errno.Msg != "" {
			return http.StatusInternalServerError, "InternalError", errno.Msg
		}
		return http.StatusInternalServerError, "InternalError", "We encountered an internal error"
	}
}
