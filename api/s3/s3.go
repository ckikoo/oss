package s3

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"oss/adaptor"
	"oss/common"
	"oss/service/dto"
	s3svc "oss/service/s3"

	"github.com/cloudwego/hertz/pkg/app"
)

type S3Ctrl struct {
	s3 s3svc.IS3Service
}

var _ IS3Handler = (*S3Ctrl)(nil)

func NewS3Ctrl(adaptor adaptor.IAdaptor) IS3Handler {
	return &S3Ctrl{s3: s3svc.NewService(adaptor)}
}

func (ctrl *S3Ctrl) ListBuckets(ctx context.Context, c *app.RequestContext) {
	ctx1, ok := common.GetUserInfoFromContext(ctx, c)
	if !ok {
		common.WriteS3Error(c, common.AuthErr, string(c.Path()))
		return
	}
	resp, errno := ctrl.s3.ListBuckets(ctx1)
	writeS3Result(c, http.StatusOK, resp, errno)
}

func (ctrl *S3Ctrl) CreateBucket(ctx context.Context, c *app.RequestContext) {
	ctx1, ok := common.GetUserInfoFromContext(ctx, c)
	if !ok {
		common.WriteS3Error(c, common.AuthErr, string(c.Path()))
		return
	}
	bucketName := c.Param("bucket_name")
	resp, errno := ctrl.s3.CreateBucket(ctx1, &dto.S3CreateBucketReq{BucketName: bucketName})
	if errno.NotOk() {
		common.WriteS3Error(c, errno, resourcePath(bucketName, ""))
		return
	}
	c.Header("Location", resp.Location)
	common.WriteS3Empty(c, http.StatusOK)
}

func (ctrl *S3Ctrl) HeadBucket(ctx context.Context, c *app.RequestContext) {
	ctx1, ok := common.GetUserInfoFromContext(ctx, c)
	if !ok {
		common.WriteS3Error(c, common.AuthErr, string(c.Path()))
		return
	}
	bucketName := c.Param("bucket_name")
	errno := ctrl.s3.HeadBucket(ctx1, bucketName)
	if errno.NotOk() {
		status, _, _ := common.MapS3Error(errno)
		c.AbortWithStatus(status)
		return
	}
	c.AbortWithStatus(http.StatusOK)
}

func (ctrl *S3Ctrl) GetBucketLocation(ctx context.Context, c *app.RequestContext) {
	ctx1, ok := common.GetUserInfoFromContext(ctx, c)
	if !ok {
		common.WriteS3Error(c, common.AuthErr, string(c.Path()))
		return
	}
	bucketName := c.Param("bucket_name")
	resp, errno := ctrl.s3.GetBucketLocation(ctx1, bucketName)
	writeS3Result(c, http.StatusOK, resp, errno)
}

func (ctrl *S3Ctrl) DeleteBucket(ctx context.Context, c *app.RequestContext) {
	ctx1, ok := common.GetUserInfoFromContext(ctx, c)
	if !ok {
		common.WriteS3Error(c, common.AuthErr, string(c.Path()))
		return
	}
	bucketName := c.Param("bucket_name")
	errno := ctrl.s3.DeleteBucket(ctx1, bucketName)
	if errno.NotOk() {
		common.WriteS3Error(c, errno, resourcePath(bucketName, ""))
		return
	}
	common.WriteS3Empty(c, http.StatusNoContent)
}

func (ctrl *S3Ctrl) ListObjectsV2(ctx context.Context, c *app.RequestContext) {
	ctx1, ok := common.GetUserInfoFromContext(ctx, c)
	if !ok {
		common.WriteS3Error(c, common.AuthErr, string(c.Path()))
		return
	}
	req := &dto.S3ListObjectsV2Req{
		BucketName:        c.Param("bucket_name"),
		Prefix:            c.Query("prefix"),
		Delimiter:         c.Query("delimiter"),
		ContinuationToken: c.Query("continuation-token"),
		StartAfter:        c.Query("start-after"),
	}
	if maxKeys := c.Query("max-keys"); maxKeys != "" {
		n, err := strconv.Atoi(maxKeys)
		if err != nil || n < 0 {
			common.WriteS3Error(c, common.ParamErr.WithMsg("invalid max-keys"), resourcePath(req.BucketName, ""))
			return
		}
		req.MaxKeys = n
	}
	resp, errno := ctrl.s3.ListObjectsV2(ctx1, req)
	writeS3Result(c, http.StatusOK, resp, errno)
}

func (ctrl *S3Ctrl) PutObject(ctx context.Context, c *app.RequestContext) {
	ctx1, ok := common.GetUserInfoFromContext(ctx, c)
	if !ok {
		common.WriteS3Error(c, common.AuthErr, string(c.Path()))
		return
	}
	bucketName, objectKey := objectPath(c)
	if string(c.GetHeader("x-amz-copy-source")) != "" {
		ctrl.CopyObject(ctx, c)
		return
	}
	body, err := requestBodyReader(c)
	if err != nil {
		common.WriteS3Error(c, common.ReadBodyError.WithErr(err), resourcePath(bucketName, objectKey))
		return
	}
	resp, errno := ctrl.s3.PutObject(ctx1, &dto.S3PutObjectReq{
		BucketName:    bucketName,
		ObjectKey:     objectKey,
		ContentLength: int64(c.Request.Header.ContentLength()),
		ContentType:   string(c.GetHeader("Content-Type")),
		StorageClass:  string(c.GetHeader("x-amz-storage-class")),
	}, body)
	if errno.NotOk() {
		common.WriteS3Error(c, errno, resourcePath(bucketName, objectKey))
		return
	}
	c.Header("ETag", resp.ETag)
	if resp.VersionID != "" {
		c.Header("x-amz-version-id", resp.VersionID)
	}
	common.WriteS3Empty(c, http.StatusOK)
}

func (ctrl *S3Ctrl) GetObject(ctx context.Context, c *app.RequestContext) {
	ctx1, ok := common.GetUserInfoFromContext(ctx, c)
	if !ok {
		common.WriteS3Error(c, common.AuthErr, string(c.Path()))
		return
	}
	bucketName, objectKey := objectPath(c)
	resp, errno := ctrl.s3.GetObject(ctx1, &dto.S3GetObjectReq{
		BucketName: bucketName,
		ObjectKey:  objectKey,
		VersionID:  c.Query("versionId"),
	})
	if errno.NotOk() {
		common.WriteS3Error(c, errno, resourcePath(bucketName, objectKey))
		return
	}
	writeObjectHeaders(c, resp)
	if resp.Body != nil {
		c.SetBodyStream(resp.Body, int(resp.ContentLength))
		return
	}
	common.WriteS3Empty(c, http.StatusOK)
}

func (ctrl *S3Ctrl) HeadObject(ctx context.Context, c *app.RequestContext) {
	ctx1, ok := common.GetUserInfoFromContext(ctx, c)
	if !ok {
		common.WriteS3Error(c, common.AuthErr, string(c.Path()))
		return
	}
	bucketName, objectKey := objectPath(c)
	resp, errno := ctrl.s3.HeadObject(ctx1, &dto.S3HeadObjectReq{
		BucketName: bucketName,
		ObjectKey:  objectKey,
		VersionID:  c.Query("versionId"),
	})
	if errno.NotOk() {
		status, _, _ := common.MapS3Error(errno)
		c.AbortWithStatus(status)
		return
	}
	writeObjectHeaders(c, resp)
	c.AbortWithStatus(http.StatusOK)
}

func (ctrl *S3Ctrl) DeleteObject(ctx context.Context, c *app.RequestContext) {
	ctx1, ok := common.GetUserInfoFromContext(ctx, c)
	if !ok {
		common.WriteS3Error(c, common.AuthErr, string(c.Path()))
		return
	}
	bucketName, objectKey := objectPath(c)
	errno := ctrl.s3.DeleteObject(ctx1, &dto.S3DeleteObjectReq{
		BucketName: bucketName,
		ObjectKey:  objectKey,
		VersionID:  c.Query("versionId"),
	})
	if errno.NotOk() {
		common.WriteS3Error(c, errno, resourcePath(bucketName, objectKey))
		return
	}
	common.WriteS3Empty(c, http.StatusNoContent)
}

func (ctrl *S3Ctrl) DeleteObjects(ctx context.Context, c *app.RequestContext) {
	ctx1, ok := common.GetUserInfoFromContext(ctx, c)
	if !ok {
		common.WriteS3Error(c, common.AuthErr, string(c.Path()))
		return
	}
	bucketName := c.Param("bucket_name")
	bodyBytes, err := c.Body()
	if err != nil {
		common.WriteS3Error(c, common.ReadBodyError.WithErr(err), resourcePath(bucketName, ""))
		return
	}
	req, err := parseDeleteObjectsXML(bodyBytes)
	if err != nil {
		common.WriteS3Error(c, common.ParamErr.WithErr(err), resourcePath(bucketName, ""))
		return
	}
	req.BucketName = bucketName
	resp, errno := ctrl.s3.DeleteObjects(ctx1, req)
	writeS3Result(c, http.StatusOK, resp, errno)
}

func (ctrl *S3Ctrl) CopyObject(ctx context.Context, c *app.RequestContext) {
	ctx1, ok := common.GetUserInfoFromContext(ctx, c)
	if !ok {
		common.WriteS3Error(c, common.AuthErr, string(c.Path()))
		return
	}
	bucketName, objectKey := objectPath(c)
	srcBucket, srcKey := parseCopySource(string(c.GetHeader("x-amz-copy-source")))
	resp, errno := ctrl.s3.CopyObject(ctx1, &dto.S3CopyObjectReq{
		BucketName:   bucketName,
		ObjectKey:    objectKey,
		SourceBucket: srcBucket,
		SourceKey:    srcKey,
	})
	writeS3Result(c, http.StatusOK, resp, errno)
}

func (ctrl *S3Ctrl) CreateMultipartUpload(ctx context.Context, c *app.RequestContext) {
	ctx1, ok := common.GetUserInfoFromContext(ctx, c)
	if !ok {
		common.WriteS3Error(c, common.AuthErr, string(c.Path()))
		return
	}
	bucketName, objectKey := objectPath(c)
	resp, errno := ctrl.s3.CreateMultipartUpload(ctx1, &dto.S3CreateMultipartUploadReq{
		BucketName:   bucketName,
		ObjectKey:    objectKey,
		ContentType:  string(c.GetHeader("Content-Type")),
		StorageClass: string(c.GetHeader("x-amz-storage-class")),
	})
	writeS3Result(c, http.StatusOK, resp, errno)
}

func (ctrl *S3Ctrl) UploadPart(ctx context.Context, c *app.RequestContext) {
	ctx1, ok := common.GetUserInfoFromContext(ctx, c)
	if !ok {
		common.WriteS3Error(c, common.AuthErr, string(c.Path()))
		return
	}
	bucketName, objectKey := objectPath(c)
	partNumber, err := strconv.ParseInt(c.Query("partNumber"), 10, 32)
	if err != nil || partNumber <= 0 {
		common.WriteS3Error(c, common.ParamErr.WithMsg("invalid partNumber"), resourcePath(bucketName, objectKey))
		return
	}
	body, err := requestBodyReader(c)
	if err != nil {
		common.WriteS3Error(c, common.ReadBodyError.WithErr(err), resourcePath(bucketName, objectKey))
		return
	}
	resp, errno := ctrl.s3.UploadPart(ctx1, &dto.S3UploadPartReq{
		BucketName:    bucketName,
		ObjectKey:     objectKey,
		UploadID:      c.Query("uploadId"),
		PartNumber:    int32(partNumber),
		ContentLength: int64(c.Request.Header.ContentLength()),
	}, body)
	if errno.NotOk() {
		common.WriteS3Error(c, errno, resourcePath(bucketName, objectKey))
		return
	}
	c.Header("ETag", resp.ETag)
	common.WriteS3Empty(c, http.StatusOK)
}

func (ctrl *S3Ctrl) ListParts(ctx context.Context, c *app.RequestContext) {
	ctx1, ok := common.GetUserInfoFromContext(ctx, c)
	if !ok {
		common.WriteS3Error(c, common.AuthErr, string(c.Path()))
		return
	}
	bucketName, objectKey := objectPath(c)
	req := &dto.S3ListPartsReq{
		BucketName: bucketName,
		ObjectKey:  objectKey,
		UploadID:   c.Query("uploadId"),
	}
	if marker := c.Query("part-number-marker"); marker != "" {
		n, err := strconv.ParseInt(marker, 10, 32)
		if err != nil || n < 0 {
			common.WriteS3Error(c, common.ParamErr.WithMsg("invalid part-number-marker"), resourcePath(bucketName, objectKey))
			return
		}
		req.PartNumberMarker = int32(n)
	}
	if maxParts := c.Query("max-parts"); maxParts != "" {
		n, err := strconv.ParseInt(maxParts, 10, 32)
		if err != nil || n <= 0 {
			common.WriteS3Error(c, common.ParamErr.WithMsg("invalid max-parts"), resourcePath(bucketName, objectKey))
			return
		}
		req.MaxParts = int32(n)
	}
	resp, errno := ctrl.s3.ListParts(ctx1, req)
	writeS3Result(c, http.StatusOK, resp, errno)
}

func (ctrl *S3Ctrl) CompleteMultipartUpload(ctx context.Context, c *app.RequestContext) {
	ctx1, ok := common.GetUserInfoFromContext(ctx, c)
	if !ok {
		common.WriteS3Error(c, common.AuthErr, string(c.Path()))
		return
	}
	bucketName, objectKey := objectPath(c)
	req := &dto.S3CompleteMultipartUploadReq{
		BucketName: bucketName,
		ObjectKey:  objectKey,
		UploadID:   c.Query("uploadId"),
	}
	bodyBytes, err := c.Body()
	if err != nil {
		common.WriteS3Error(c, common.ReadBodyError.WithErr(err), resourcePath(bucketName, objectKey))
		return
	}
	parts, err := parseCompleteMultipartUploadXML(bodyBytes)
	if err != nil {
		common.WriteS3Error(c, common.ParamErr.WithErr(err), resourcePath(bucketName, objectKey))
		return
	}
	req.Parts = parts
	resp, errno := ctrl.s3.CompleteMultipartUpload(ctx1, req)
	writeS3Result(c, http.StatusOK, resp, errno)
}

func (ctrl *S3Ctrl) AbortMultipartUpload(ctx context.Context, c *app.RequestContext) {
	ctx1, ok := common.GetUserInfoFromContext(ctx, c)
	if !ok {
		common.WriteS3Error(c, common.AuthErr, string(c.Path()))
		return
	}
	bucketName, objectKey := objectPath(c)
	errno := ctrl.s3.AbortMultipartUpload(ctx1, &dto.S3AbortMultipartUploadReq{
		BucketName: bucketName,
		ObjectKey:  objectKey,
		UploadID:   c.Query("uploadId"),
	})
	if errno.NotOk() {
		common.WriteS3Error(c, errno, resourcePath(bucketName, objectKey))
		return
	}
	common.WriteS3Empty(c, http.StatusNoContent)
}

func parseCompleteMultipartUploadXML(body []byte) ([]dto.S3CompleteMultipartPartItem, error) {
	var req struct {
		Parts []dto.S3CompleteMultipartPartItem `xml:"Part"`
	}
	if err := xml.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	if len(req.Parts) == 0 {
		return nil, common.ParamErr.WithMsg("multipart complete parts is required")
	}
	for _, part := range req.Parts {
		if part.PartNumber <= 0 || strings.TrimSpace(part.ETag) == "" {
			return nil, common.ParamErr.WithMsg("invalid multipart complete part")
		}
	}
	return req.Parts, nil
}

func parseDeleteObjectsXML(body []byte) (*dto.S3DeleteObjectsReq, error) {
	var req dto.S3DeleteObjectsReq
	if err := xml.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	if len(req.Objects) == 0 {
		return nil, common.ParamErr.WithMsg("delete objects is required")
	}
	if len(req.Objects) > 1000 {
		return nil, common.ParamErr.WithMsg("delete objects exceeds limit")
	}
	for _, item := range req.Objects {
		if strings.TrimSpace(item.Key) == "" {
			return nil, common.ParamErr.WithMsg("object key is required")
		}
	}
	return &req, nil
}

func requestBodyReader(c *app.RequestContext) (io.Reader, error) {
	if c.Request.IsBodyStream() {
		return c.RequestBodyStream(), nil
	}
	return bytes.NewReader(c.Request.Body()), nil
}

func writeS3Result(c *app.RequestContext, status int, resp any, errno common.Errno) {
	if errno.NotOk() {
		if errno.Code == 501 {
			common.WriteS3ErrorCode(c, http.StatusNotImplemented, "NotImplemented", "A header you provided implies functionality that is not implemented", string(c.Path()))
			return
		}
		common.WriteS3Error(c, errno, string(c.Path()))
		return
	}
	common.WriteS3XML(c, status, resp)
}

func objectPath(c *app.RequestContext) (string, string) {
	return c.Param("bucket_name"), strings.TrimPrefix(c.Param("object_key"), "/")
}

func resourcePath(bucketName, objectKey string) string {
	if objectKey == "" {
		return "/" + bucketName
	}
	return "/" + bucketName + "/" + objectKey
}

func writeObjectHeaders(c *app.RequestContext, resp *dto.S3GetObjectResp) {
	if resp.ContentType != "" {
		c.Header("Content-Type", resp.ContentType)
	}
	if resp.ContentLength >= 0 {
		c.Header("Content-Length", strconv.FormatInt(resp.ContentLength, 10))
	}
	if resp.ETag != "" {
		c.Header("ETag", resp.ETag)
	}
	if resp.LastModified != "" {
		c.Header("Last-Modified", resp.LastModified)
	}
	if resp.VersionID != "" {
		c.Header("x-amz-version-id", resp.VersionID)
	}
}

func parseCopySource(src string) (string, string) {
	src = strings.TrimPrefix(strings.TrimSpace(src), "/")
	parts := strings.SplitN(src, "/", 2)
	if len(parts) != 2 {
		return unescapeCopySourcePart(src), ""
	}
	key := parts[1]
	if idx := strings.IndexByte(key, '?'); idx >= 0 {
		key = key[:idx]
	}
	return unescapeCopySourcePart(parts[0]), unescapeCopySourcePart(key)
}

func unescapeCopySourcePart(s string) string {
	out, err := url.PathUnescape(s)
	if err != nil {
		return s
	}
	return out
}
