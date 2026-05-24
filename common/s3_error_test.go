package common

import (
	"encoding/xml"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
)

func TestMapS3Error(t *testing.T) {
	tests := []struct {
		name       string
		errno      Errno
		wantStatus int
		wantCode   string
	}{
		{name: "auth", errno: AuthErr, wantStatus: http.StatusForbidden, wantCode: "AccessDenied"},
		{name: "bucket not found", errno: BucketNotFoundErr, wantStatus: http.StatusNotFound, wantCode: "NoSuchBucket"},
		{name: "key not found", errno: ResouceNotFoundErr, wantStatus: http.StatusNotFound, wantCode: "NoSuchKey"},
		{name: "bad argument", errno: ParamErr, wantStatus: http.StatusBadRequest, wantCode: "InvalidArgument"},
		{name: "upload not found", errno: FileUploadIdNotFound, wantStatus: http.StatusNotFound, wantCode: "NoSuchUpload"},
		{name: "internal", errno: DatabaseErr, wantStatus: http.StatusInternalServerError, wantCode: "InternalError"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, code, msg := MapS3Error(tt.errno)
			if status != tt.wantStatus || code != tt.wantCode || msg == "" {
				t.Fatalf("MapS3Error = (%d, %q, %q), want status=%d code=%q with message", status, code, msg, tt.wantStatus, tt.wantCode)
			}
		})
	}
}

func TestWriteS3ErrorCodeWritesXMLResponse(t *testing.T) {
	var c app.RequestContext
	c.Request.SetRequestURI("/bucket/key")
	c.Request.Header.Set("X-Request-Id", "req-1")

	WriteS3ErrorCode(&c, http.StatusNotFound, "NoSuchKey", "missing", "/bucket/key")

	if got := c.Response.StatusCode(); got != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", got, http.StatusNotFound)
	}
	if got := string(c.Response.Header.ContentType()); got != S3XMLContentType {
		t.Fatalf("content-type = %q, want %q", got, S3XMLContentType)
	}
	body := string(c.Response.Body())
	if !strings.HasPrefix(body, xml.Header) {
		t.Fatalf("body should start with XML header, got %q", body)
	}

	var parsed S3Error
	if err := xml.Unmarshal(c.Response.Body(), &parsed); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if parsed.Code != "NoSuchKey" || parsed.Message != "missing" || parsed.Resource != "/bucket/key" || parsed.RequestID != "req-1" {
		t.Fatalf("unexpected S3 error body: %+v", parsed)
	}
}
