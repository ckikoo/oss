package s3

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"oss/adaptor"
	"oss/common"
	"oss/consts"
	"oss/service/bucket"
	"oss/service/do"
	"oss/service/dto"
	"oss/service/multipart"
	"oss/service/object"

	"github.com/samber/lo"
)

type Service struct {
	bucket    *bucket.Service
	object    *object.Service
	multipart *multipart.Service
}

var _ IS3Service = (*Service)(nil)

func NewService(adaptor adaptor.IAdaptor) IS3Service {
	return &Service{
		bucket:    bucket.NewService(adaptor),
		object:    object.NewService(adaptor),
		multipart: multipart.NewService(adaptor),
	}
}

func (srv *Service) ListBuckets(ctx *common.UserInfoCtx) (*dto.S3ListBucketsResp, common.Errno) {
	resp, errno := srv.bucket.ListBuckets(ctx, &dto.ListBucketsReq{Status: consts.BucketStatusNormal})
	if errno.NotOk() {
		return nil, errno
	}

	items := lo.Map(resp.Items, func(item *dto.BucketItem, _ int) dto.S3Bucket {
		return dto.S3Bucket{
			Name:         item.Name,
			CreationDate: formatS3Time(item.CreatedAt),
		}
	})

	return &dto.S3ListBucketsResp{
		Xmlns: "http://s3.amazonaws.com/doc/2006-03-01/",
		Owner: dto.S3Owner{
			ID:          strconv.FormatInt(ctx.UserID, 10),
			DisplayName: ctx.AccessKey,
		},
		Buckets: items,
	}, common.OK
}

func (srv *Service) CreateBucket(ctx *common.UserInfoCtx, req *dto.S3CreateBucketReq) (*dto.S3CreateBucketResp, common.Errno) {
	_, errno := srv.bucket.CreateBucket(ctx, &dto.CreateBucketReq{
		UserID:       ctx.UserID,
		Name:         req.BucketName,
		Region:       req.Region,
		Acl:          consts.BucketAclPrivate,
		Versioning:   consts.BucketVersioningDisabled,
		StorageClass: consts.StorageClassStandard,
	})
	if errno.NotOk() {
		return nil, errno
	}
	return &dto.S3CreateBucketResp{Location: "/" + req.BucketName}, common.OK
}

func (srv *Service) HeadBucket(ctx *common.UserInfoCtx, bucketName string) common.Errno {
	_, errno := srv.bucket.GetBucket(ctx, bucketName)
	return errno
}

func (srv *Service) GetBucketLocation(ctx *common.UserInfoCtx, bucketName string) (*dto.S3GetBucketLocationResp, common.Errno) {
	info, errno := srv.bucket.GetBucket(ctx, bucketName)
	if errno.NotOk() {
		return nil, errno
	}
	return &dto.S3GetBucketLocationResp{
		Xmlns:              "http://s3.amazonaws.com/doc/2006-03-01/",
		LocationConstraint: normalizeS3LocationConstraint(info.Region),
	}, common.OK
}

func (srv *Service) DeleteBucket(ctx *common.UserInfoCtx, bucketName string) common.Errno {
	info, errno := srv.bucket.GetBucket(ctx, bucketName)
	if errno.NotOk() {
		return errno
	}
	return srv.bucket.DeleteBucket(ctx, info.ID, bucketName)
}

func (srv *Service) ListObjectsV2(ctx *common.UserInfoCtx, req *dto.S3ListObjectsV2Req) (*dto.S3ListObjectsV2Resp, common.Errno) {
	if errno := srv.HeadBucket(ctx, req.BucketName); errno.NotOk() {
		return nil, errno
	}

	maxKeys := req.MaxKeys
	if maxKeys <= 0 {
		maxKeys = consts.DefaultMaxKeys
	}
	if maxKeys > consts.DefaultMaxKeys {
		maxKeys = consts.DefaultMaxKeys
	}

	marker := req.StartAfter
	if req.ContinuationToken != "" {
		marker = req.ContinuationToken
	}

	resp, errno := srv.object.ListObjects(ctx, &dto.ListObjectsReq{
		BucketName: req.BucketName,
		Prefix:     req.Prefix,
		Marker:     marker,
		MaxKeys:    maxKeys + 1,
	})
	if errno.NotOk() {
		return nil, errno
	}

	items := resp.Items
	truncated := len(items) > maxKeys
	if truncated {
		items = items[:maxKeys]
	}

	return buildS3ListObjectsV2Resp(req, items, maxKeys, truncated), common.OK
}

func buildS3ListObjectsV2Resp(req *dto.S3ListObjectsV2Req, items []*dto.ObjectItem, maxKeys int, truncated bool) *dto.S3ListObjectsV2Resp {
	contents := make([]dto.S3Object, 0, len(items))
	commonPrefixes := make([]dto.S3CommonPrefix, 0)
	prefixSeen := map[string]struct{}{}
	for _, item := range items {
		if req.Delimiter != "" {
			rest := strings.TrimPrefix(item.ObjectKey, req.Prefix)
			if idx := strings.Index(rest, req.Delimiter); idx >= 0 {
				prefix := req.Prefix + rest[:idx+len(req.Delimiter)]
				if _, ok := prefixSeen[prefix]; !ok {
					prefixSeen[prefix] = struct{}{}
					commonPrefixes = append(commonPrefixes, dto.S3CommonPrefix{Prefix: prefix})
				}
				continue
			}
		}
		contents = append(contents, dto.S3Object{
			Key:          item.ObjectKey,
			LastModified: formatS3Time(item.LastModified),
			ETag:         quoteETag(item.Etag),
			Size:         item.Size,
			StorageClass: item.StorageClass,
		})
	}
	sort.Slice(commonPrefixes, func(i, j int) bool { return commonPrefixes[i].Prefix < commonPrefixes[j].Prefix })

	out := &dto.S3ListObjectsV2Resp{
		Xmlns:          "http://s3.amazonaws.com/doc/2006-03-01/",
		Name:           req.BucketName,
		Prefix:         req.Prefix,
		KeyCount:       len(contents) + len(commonPrefixes),
		MaxKeys:        maxKeys,
		Delimiter:      req.Delimiter,
		IsTruncated:    truncated,
		Contents:       contents,
		CommonPrefixes: commonPrefixes,
		StartAfter:     req.StartAfter,
	}
	if req.ContinuationToken != "" {
		out.ContinuationToken = req.ContinuationToken
	}
	if truncated && len(items) > 0 {
		out.NextContinuationToken = items[len(items)-1].ObjectKey
	}
	return out
}

func (srv *Service) PutObject(ctx *common.UserInfoCtx, req *dto.S3PutObjectReq, body io.Reader) (*dto.S3PutObjectResp, common.Errno) {
	resp, errno := srv.object.PutObjectStream(ctx, &dto.PutObjectStreamReq{
		BucketName:    req.BucketName,
		ObjectKey:     req.ObjectKey,
		ContentType:   req.ContentType,
		StorageClass:  req.StorageClass,
		Acl:           consts.ObjectAclInheritBucket,
		Overwrite:     true,
		ContentLength: req.ContentLength,
	}, body)
	if errno.NotOk() {
		return nil, errno
	}
	return &dto.S3PutObjectResp{ETag: quoteETag(resp.Etag), VersionID: resp.VersionID}, common.OK
}

func (srv *Service) GetObject(ctx *common.UserInfoCtx, req *dto.S3GetObjectReq) (*dto.S3GetObjectResp, common.Errno) {
	resp, errno := srv.object.GetObjectStream(ctx, req.BucketName, req.ObjectKey, req.VersionID)
	if errno.NotOk() {
		return nil, errno
	}
	return &dto.S3GetObjectResp{
		ContentLength: resp.ContentLength,
		ContentType:   resp.ContentType,
		ETag:          quoteETag(resp.Etag),
		VersionID:     resp.VersionID,
		LastModified:  formatS3Time(resp.LastModified),
		Body:          resp.Body,
	}, common.OK
}

func (srv *Service) HeadObject(ctx *common.UserInfoCtx, req *dto.S3HeadObjectReq) (*dto.S3HeadObjectResp, common.Errno) {
	resp, errno := srv.object.GetObjectMetadata(ctx, req.BucketName, req.ObjectKey, req.VersionID)
	if errno.NotOk() {
		return nil, errno
	}
	return &dto.S3HeadObjectResp{
		ContentLength: resp.Size,
		ContentType:   contentTypeOrDefault(resp.ContentType),
		ETag:          quoteETag(resp.Etag),
		VersionID:     resp.VersionID,
	}, common.OK
}

func (srv *Service) DeleteObject(ctx *common.UserInfoCtx, req *dto.S3DeleteObjectReq) common.Errno {
	return srv.object.DeleteObject(ctx, req.BucketName, req.ObjectKey, req.VersionID)
}

func (srv *Service) DeleteObjects(ctx *common.UserInfoCtx, req *dto.S3DeleteObjectsReq) (*dto.S3DeleteObjectsResp, common.Errno) {
	if len(req.Objects) == 0 {
		return nil, common.ParamErr.WithMsg("delete objects is required")
	}

	resp := &dto.S3DeleteObjectsResp{}
	for _, item := range req.Objects {
		if strings.TrimSpace(item.Key) == "" {
			resp.Errors = append(resp.Errors, dto.S3DeleteObjectsErrorItem{
				Key:       item.Key,
				VersionID: item.VersionID,
				Code:      "InvalidArgument",
				Message:   "Object key is required",
			})
			continue
		}

		errno := srv.object.DeleteObject(ctx, req.BucketName, item.Key, item.VersionID)
		if !errno.NotOk() || errno.Code == common.ResouceNotFoundErr.Code {
			if !req.Quiet {
				resp.Deleted = append(resp.Deleted, dto.S3DeleteObjectsDeletedItem{
					Key:       item.Key,
					VersionID: item.VersionID,
				})
			}
			continue
		}

		_, code, msg := common.MapS3Error(errno)
		resp.Errors = append(resp.Errors, dto.S3DeleteObjectsErrorItem{
			Key:       item.Key,
			VersionID: item.VersionID,
			Code:      code,
			Message:   msg,
		})
	}

	return resp, common.OK
}

func (srv *Service) CopyObject(ctx *common.UserInfoCtx, req *dto.S3CopyObjectReq) (*dto.S3CopyObjectResp, common.Errno) {
	if req.SourceBucket == "" || req.SourceKey == "" {
		return nil, common.ParamErr.WithMsg("copy source is required")
	}

	source, errno := srv.object.GetObjectStream(ctx, req.SourceBucket, req.SourceKey, "")
	if errno.NotOk() {
		return nil, errno
	}
	defer source.Body.Close()

	resp, errno := srv.object.PutObjectStream(ctx, &dto.PutObjectStreamReq{
		BucketName:    req.BucketName,
		ObjectKey:     req.ObjectKey,
		ContentType:   source.ContentType,
		StorageClass:  source.StorageClass,
		Acl:           consts.ObjectAclInheritBucket,
		Overwrite:     true,
		ContentLength: source.ContentLength,
	}, source.Body)
	if errno.NotOk() {
		return nil, errno
	}

	return &dto.S3CopyObjectResp{
		LastModified: time.Now().UTC().Format(time.RFC3339),
		ETag:         quoteETag(resp.Etag),
	}, common.OK
}

func (srv *Service) CreateMultipartUpload(ctx *common.UserInfoCtx, req *dto.S3CreateMultipartUploadReq) (*dto.S3CreateMultipartUploadResp, common.Errno) {
	resp, errno := srv.multipart.CreateMultipartUpload(ctx, req.BucketName, &dto.CreateMultipartUploadReq{
		ObjectKey:    req.ObjectKey,
		ContentType:  req.ContentType,
		StorageClass: req.StorageClass,
		Overwrite:    true,
	})
	if errno.NotOk() {
		return nil, errno
	}
	return &dto.S3CreateMultipartUploadResp{
		Bucket:   req.BucketName,
		Key:      req.ObjectKey,
		UploadID: resp.UploadID,
	}, common.OK
}

func (srv *Service) UploadPart(ctx *common.UserInfoCtx, req *dto.S3UploadPartReq, body io.Reader) (*dto.S3UploadPartResp, common.Errno) {
	resp, errno := srv.multipart.UploadMultipartPart(ctx, req.UploadID, "", req.UploadID, req.PartNumber, body, req.ContentLength)
	if errno.NotOk() {
		return nil, errno
	}
	return &dto.S3UploadPartResp{ETag: quoteETag(resp.Etag)}, common.OK
}

func (srv *Service) ListParts(ctx *common.UserInfoCtx, req *dto.S3ListPartsReq) (*dto.S3ListPartsResp, common.Errno) {
	upload, parts, errno := srv.multipart.ListParts(ctx, req.UploadID)
	if errno.NotOk() {
		return nil, errno
	}
	if upload.BucketName != req.BucketName || upload.ObjectKey != req.ObjectKey {
		return nil, common.FileUploadIdNotFound
	}

	return buildS3ListPartsResp(req, parts), common.OK
}

func buildS3ListPartsResp(req *dto.S3ListPartsReq, parts []*do.MultipartPartDo) *dto.S3ListPartsResp {
	maxParts := req.MaxParts
	if maxParts <= 0 {
		maxParts = 1000
	}
	if maxParts > 1000 {
		maxParts = 1000
	}

	start := 0
	if req.PartNumberMarker > 0 {
		for start < len(parts) && parts[start].PartNumber <= req.PartNumberMarker {
			start++
		}
	}
	end := start + int(maxParts)
	truncated := false
	if end < len(parts) {
		truncated = true
	} else {
		end = len(parts)
	}

	outParts := make([]dto.S3Part, 0, end-start)
	var nextMarker int32
	for _, part := range parts[start:end] {
		outParts = append(outParts, dto.S3Part{
			PartNumber:   part.PartNumber,
			LastModified: part.CreatedAt.UTC().Format(time.RFC3339),
			ETag:         quoteETag(part.Etag),
			Size:         part.Size,
		})
		nextMarker = part.PartNumber
	}

	return &dto.S3ListPartsResp{
		Bucket:               req.BucketName,
		Key:                  req.ObjectKey,
		UploadID:             req.UploadID,
		PartNumberMarker:     req.PartNumberMarker,
		NextPartNumberMarker: nextMarker,
		MaxParts:             maxParts,
		IsTruncated:          truncated,
		Parts:                outParts,
	}
}

func (srv *Service) CompleteMultipartUpload(ctx *common.UserInfoCtx, req *dto.S3CompleteMultipartUploadReq) (*dto.S3CompleteMultipartUploadResp, common.Errno) {
	parts := make([]dto.MultipartCompletePart, 0, len(req.Parts))
	for _, part := range req.Parts {
		parts = append(parts, dto.MultipartCompletePart{
			PartNumber: part.PartNumber,
			Etag:       strings.Trim(part.ETag, "\""),
		})
	}

	resp, errno := srv.multipart.CompleteMultipartUpload(ctx, req.UploadID, req.BucketName, &dto.CompleteMultipartUploadReq{Parts: parts})
	if errno.NotOk() {
		return nil, errno
	}
	return &dto.S3CompleteMultipartUploadResp{
		Location: resource(req.BucketName, req.ObjectKey),
		Bucket:   req.BucketName,
		Key:      req.ObjectKey,
		ETag:     quoteETag(resp.Etag),
	}, common.OK
}

func (srv *Service) AbortMultipartUpload(ctx *common.UserInfoCtx, req *dto.S3AbortMultipartUploadReq) common.Errno {
	return srv.multipart.AbortMultipartUpload(ctx, req.UploadID)
}

func notImplemented() common.Errno {
	return common.Errno{Code: 501, Msg: "Not Implemented"}
}

func formatS3Time(ms int64) string {
	if ms <= 0 {
		return time.Now().UTC().Format(time.RFC3339)
	}
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}

func quoteETag(etag string) string {
	if etag == "" || strings.HasPrefix(etag, "\"") {
		return etag
	}
	return fmt.Sprintf("\"%s\"", etag)
}

func contentTypeOrDefault(contentType string) string {
	if contentType == "" {
		return "application/octet-stream"
	}
	return contentType
}

func normalizeS3LocationConstraint(region string) string {
	region = strings.TrimSpace(region)
	if region == "" || region == "us-east-1" {
		return ""
	}
	return region
}

func resource(bucketName, objectKey string) string {
	if objectKey == "" {
		return fmt.Sprintf("/%s", bucketName)
	}
	return fmt.Sprintf("/%s/%s", bucketName, objectKey)
}
