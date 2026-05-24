package router

import (
	"context"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
)

type fakeS3Handler struct {
	called string
}

func (h *fakeS3Handler) record(name string) {
	h.called = name
}

func (h *fakeS3Handler) ListBuckets(context.Context, *app.RequestContext)  { h.record("ListBuckets") }
func (h *fakeS3Handler) CreateBucket(context.Context, *app.RequestContext) { h.record("CreateBucket") }
func (h *fakeS3Handler) HeadBucket(context.Context, *app.RequestContext)   { h.record("HeadBucket") }
func (h *fakeS3Handler) GetBucketLocation(context.Context, *app.RequestContext) {
	h.record("GetBucketLocation")
}
func (h *fakeS3Handler) DeleteBucket(context.Context, *app.RequestContext) { h.record("DeleteBucket") }
func (h *fakeS3Handler) ListObjectsV2(context.Context, *app.RequestContext) {
	h.record("ListObjectsV2")
}
func (h *fakeS3Handler) PutObject(context.Context, *app.RequestContext)    { h.record("PutObject") }
func (h *fakeS3Handler) GetObject(context.Context, *app.RequestContext)    { h.record("GetObject") }
func (h *fakeS3Handler) HeadObject(context.Context, *app.RequestContext)   { h.record("HeadObject") }
func (h *fakeS3Handler) DeleteObject(context.Context, *app.RequestContext) { h.record("DeleteObject") }
func (h *fakeS3Handler) DeleteObjects(context.Context, *app.RequestContext) {
	h.record("DeleteObjects")
}
func (h *fakeS3Handler) CopyObject(context.Context, *app.RequestContext) { h.record("CopyObject") }
func (h *fakeS3Handler) CreateMultipartUpload(context.Context, *app.RequestContext) {
	h.record("CreateMultipartUpload")
}
func (h *fakeS3Handler) UploadPart(context.Context, *app.RequestContext) { h.record("UploadPart") }
func (h *fakeS3Handler) ListParts(context.Context, *app.RequestContext)  { h.record("ListParts") }
func (h *fakeS3Handler) CompleteMultipartUpload(context.Context, *app.RequestContext) {
	h.record("CompleteMultipartUpload")
}
func (h *fakeS3Handler) AbortMultipartUpload(context.Context, *app.RequestContext) {
	h.record("AbortMultipartUpload")
}

func TestDispatchS3ObjectPutRoutesMultipartCopyAndPlainPut(t *testing.T) {
	tests := []struct {
		name       string
		uri        string
		copySource string
		want       string
	}{
		{name: "upload part", uri: "/bucket/key?partNumber=1&uploadId=u1", want: "UploadPart"},
		{name: "copy object", uri: "/bucket/key", copySource: "/src/key", want: "CopyObject"},
		{name: "put object", uri: "/bucket/key", want: "PutObject"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &fakeS3Handler{}
			var c app.RequestContext
			c.Request.SetRequestURI(tt.uri)
			if tt.copySource != "" {
				c.Request.Header.Set("x-amz-copy-source", tt.copySource)
			}

			dispatchS3ObjectPut(RouterDeps{S3Handler: h})(context.Background(), &c)

			if h.called != tt.want {
				t.Fatalf("called = %q, want %q", h.called, tt.want)
			}
		})
	}
}

func TestDispatchS3BucketGetRoutesLocationAndListObjects(t *testing.T) {
	tests := []struct {
		name string
		uri  string
		want string
	}{
		{name: "location", uri: "/bucket?location", want: "GetBucketLocation"},
		{name: "list objects v2", uri: "/bucket?list-type=2", want: "ListObjectsV2"},
		{name: "default list objects", uri: "/bucket", want: "ListObjectsV2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &fakeS3Handler{}
			var c app.RequestContext
			c.Request.SetRequestURI(tt.uri)

			dispatchS3BucketGet(RouterDeps{S3Handler: h})(context.Background(), &c)

			if h.called != tt.want {
				t.Fatalf("called = %q, want %q", h.called, tt.want)
			}
		})
	}
}

func TestDispatchS3BucketPostRoutesDeleteObjects(t *testing.T) {
	h := &fakeS3Handler{}
	var c app.RequestContext
	c.Request.SetRequestURI("/bucket?delete")

	dispatchS3BucketPost(RouterDeps{S3Handler: h})(context.Background(), &c)

	if h.called != "DeleteObjects" {
		t.Fatalf("called = %q, want DeleteObjects", h.called)
	}
}

func TestDispatchS3ObjectPostRoutesMultipartLifecycle(t *testing.T) {
	tests := []struct {
		name string
		uri  string
		want string
	}{
		{name: "create multipart", uri: "/bucket/key?uploads", want: "CreateMultipartUpload"},
		{name: "complete multipart", uri: "/bucket/key?uploadId=u1", want: "CompleteMultipartUpload"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &fakeS3Handler{}
			var c app.RequestContext
			c.Request.SetRequestURI(tt.uri)

			dispatchS3ObjectPost(RouterDeps{S3Handler: h})(context.Background(), &c)

			if h.called != tt.want {
				t.Fatalf("called = %q, want %q", h.called, tt.want)
			}
		})
	}
}

func TestDispatchS3ObjectGetAndDeleteRouteMultipartQueries(t *testing.T) {
	tests := []struct {
		name     string
		dispatch func(RouterDeps) app.HandlerFunc
		uri      string
		want     string
	}{
		{name: "list parts", dispatch: dispatchS3ObjectGet, uri: "/bucket/key?uploadId=u1", want: "ListParts"},
		{name: "get object", dispatch: dispatchS3ObjectGet, uri: "/bucket/key", want: "GetObject"},
		{name: "abort multipart", dispatch: dispatchS3ObjectDelete, uri: "/bucket/key?uploadId=u1", want: "AbortMultipartUpload"},
		{name: "delete object", dispatch: dispatchS3ObjectDelete, uri: "/bucket/key", want: "DeleteObject"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &fakeS3Handler{}
			var c app.RequestContext
			c.Request.SetRequestURI(tt.uri)

			tt.dispatch(RouterDeps{S3Handler: h})(context.Background(), &c)

			if h.called != tt.want {
				t.Fatalf("called = %q, want %q", h.called, tt.want)
			}
		})
	}
}
