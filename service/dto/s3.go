package dto

import (
	"encoding/xml"
	"io"
)

type S3Owner struct {
	ID          string `xml:"ID" json:"id"`
	DisplayName string `xml:"DisplayName" json:"display_name"`
}

type S3Bucket struct {
	Name         string `xml:"Name" json:"name"`
	CreationDate string `xml:"CreationDate" json:"creation_date"`
}

type S3ListBucketsResp struct {
	XMLName xml.Name   `xml:"ListAllMyBucketsResult" json:"-"`
	Xmlns   string     `xml:"xmlns,attr,omitempty" json:"-"`
	Owner   S3Owner    `xml:"Owner" json:"owner"`
	Buckets []S3Bucket `xml:"Buckets>Bucket" json:"buckets"`
}

type S3CreateBucketReq struct {
	BucketName string `json:"bucket_name"`
	Region     string `json:"region,omitempty"`
}

type S3CreateBucketResp struct {
	Location string `json:"location"`
}

type S3GetBucketLocationResp struct {
	XMLName            xml.Name `xml:"LocationConstraint" json:"-"`
	Xmlns              string   `xml:"xmlns,attr,omitempty" json:"-"`
	LocationConstraint string   `xml:",chardata" json:"location_constraint"`
}

type S3ListObjectsV2Req struct {
	BucketName        string `json:"bucket_name"`
	Prefix            string `json:"prefix,omitempty"`
	Delimiter         string `json:"delimiter,omitempty"`
	ContinuationToken string `json:"continuation_token,omitempty"`
	MaxKeys           int    `json:"max_keys,omitempty"`
	StartAfter        string `json:"start_after,omitempty"`
}

type S3Object struct {
	Key          string `xml:"Key" json:"key"`
	LastModified string `xml:"LastModified" json:"last_modified"`
	ETag         string `xml:"ETag" json:"etag"`
	Size         int64  `xml:"Size" json:"size"`
	StorageClass string `xml:"StorageClass" json:"storage_class"`
}

type S3CommonPrefix struct {
	Prefix string `xml:"Prefix" json:"prefix"`
}

type S3ListObjectsV2Resp struct {
	XMLName               xml.Name         `xml:"ListBucketResult" json:"-"`
	Xmlns                 string           `xml:"xmlns,attr,omitempty" json:"-"`
	Name                  string           `xml:"Name" json:"name"`
	Prefix                string           `xml:"Prefix" json:"prefix"`
	KeyCount              int              `xml:"KeyCount" json:"key_count"`
	MaxKeys               int              `xml:"MaxKeys" json:"max_keys"`
	Delimiter             string           `xml:"Delimiter,omitempty" json:"delimiter,omitempty"`
	IsTruncated           bool             `xml:"IsTruncated" json:"is_truncated"`
	Contents              []S3Object       `xml:"Contents" json:"contents"`
	CommonPrefixes        []S3CommonPrefix `xml:"CommonPrefixes" json:"common_prefixes"`
	ContinuationToken     string           `xml:"ContinuationToken,omitempty" json:"continuation_token,omitempty"`
	NextContinuationToken string           `xml:"NextContinuationToken,omitempty" json:"next_continuation_token,omitempty"`
	StartAfter            string           `xml:"StartAfter,omitempty" json:"start_after,omitempty"`
}

type S3PutObjectReq struct {
	BucketName    string `json:"bucket_name"`
	ObjectKey     string `json:"object_key"`
	ContentLength int64  `json:"content_length"`
	ContentType   string `json:"content_type,omitempty"`
	StorageClass  string `json:"storage_class,omitempty"`
}

type S3PutObjectResp struct {
	ETag      string `json:"etag"`
	VersionID string `json:"version_id,omitempty"`
}

type S3GetObjectReq struct {
	BucketName string `json:"bucket_name"`
	ObjectKey  string `json:"object_key"`
	VersionID  string `json:"version_id,omitempty"`
}

type S3GetObjectResp struct {
	ContentLength int64         `json:"content_length"`
	ContentType   string        `json:"content_type,omitempty"`
	ETag          string        `json:"etag"`
	VersionID     string        `json:"version_id,omitempty"`
	LastModified  string        `json:"last_modified,omitempty"`
	Body          io.ReadCloser `json:"-"`
}

type S3HeadObjectReq = S3GetObjectReq
type S3HeadObjectResp = S3GetObjectResp

type S3DeleteObjectReq struct {
	BucketName string `json:"bucket_name"`
	ObjectKey  string `json:"object_key"`
	VersionID  string `json:"version_id,omitempty"`
}

type S3DeleteObjectsReq struct {
	BucketName string                   `json:"bucket_name"`
	Quiet      bool                     `xml:"Quiet" json:"quiet,omitempty"`
	Objects    []S3DeleteObjectsReqItem `xml:"Object" json:"objects"`
}

type S3DeleteObjectsReqItem struct {
	Key       string `xml:"Key" json:"key"`
	VersionID string `xml:"VersionId,omitempty" json:"version_id,omitempty"`
}

type S3DeleteObjectsResp struct {
	XMLName xml.Name                     `xml:"DeleteResult" json:"-"`
	Deleted []S3DeleteObjectsDeletedItem `xml:"Deleted,omitempty" json:"deleted,omitempty"`
	Errors  []S3DeleteObjectsErrorItem   `xml:"Error,omitempty" json:"errors,omitempty"`
}

type S3DeleteObjectsDeletedItem struct {
	Key       string `xml:"Key" json:"key"`
	VersionID string `xml:"VersionId,omitempty" json:"version_id,omitempty"`
}

type S3DeleteObjectsErrorItem struct {
	Key       string `xml:"Key" json:"key"`
	VersionID string `xml:"VersionId,omitempty" json:"version_id,omitempty"`
	Code      string `xml:"Code" json:"code"`
	Message   string `xml:"Message" json:"message"`
}

type S3CopyObjectReq struct {
	BucketName   string `json:"bucket_name"`
	ObjectKey    string `json:"object_key"`
	SourceBucket string `json:"source_bucket"`
	SourceKey    string `json:"source_key"`
}

type S3CopyObjectResp struct {
	XMLName      xml.Name `xml:"CopyObjectResult" json:"-"`
	LastModified string   `xml:"LastModified" json:"last_modified"`
	ETag         string   `xml:"ETag" json:"etag"`
}

type S3CreateMultipartUploadReq struct {
	BucketName   string `json:"bucket_name"`
	ObjectKey    string `json:"object_key"`
	ContentType  string `json:"content_type,omitempty"`
	StorageClass string `json:"storage_class,omitempty"`
}

type S3CreateMultipartUploadResp struct {
	XMLName  xml.Name `xml:"InitiateMultipartUploadResult" json:"-"`
	Bucket   string   `xml:"Bucket" json:"bucket"`
	Key      string   `xml:"Key" json:"key"`
	UploadID string   `xml:"UploadId" json:"upload_id"`
}

type S3UploadPartReq struct {
	BucketName    string `json:"bucket_name"`
	ObjectKey     string `json:"object_key"`
	UploadID      string `json:"upload_id"`
	PartNumber    int32  `json:"part_number"`
	ContentLength int64  `json:"content_length"`
}

type S3UploadPartResp struct {
	ETag string `json:"etag"`
}

type S3ListPartsReq struct {
	BucketName       string `json:"bucket_name"`
	ObjectKey        string `json:"object_key"`
	UploadID         string `json:"upload_id"`
	PartNumberMarker int32  `json:"part_number_marker,omitempty"`
	MaxParts         int32  `json:"max_parts,omitempty"`
}

type S3Part struct {
	PartNumber   int32  `xml:"PartNumber" json:"part_number"`
	LastModified string `xml:"LastModified" json:"last_modified"`
	ETag         string `xml:"ETag" json:"etag"`
	Size         int64  `xml:"Size" json:"size"`
}

type S3ListPartsResp struct {
	XMLName              xml.Name `xml:"ListPartsResult" json:"-"`
	Bucket               string   `xml:"Bucket" json:"bucket"`
	Key                  string   `xml:"Key" json:"key"`
	UploadID             string   `xml:"UploadId" json:"upload_id"`
	PartNumberMarker     int32    `xml:"PartNumberMarker" json:"part_number_marker"`
	NextPartNumberMarker int32    `xml:"NextPartNumberMarker" json:"next_part_number_marker"`
	MaxParts             int32    `xml:"MaxParts" json:"max_parts"`
	IsTruncated          bool     `xml:"IsTruncated" json:"is_truncated"`
	Parts                []S3Part `xml:"Part" json:"parts"`
}

type S3CompleteMultipartUploadReq struct {
	BucketName string                        `json:"bucket_name"`
	ObjectKey  string                        `json:"object_key"`
	UploadID   string                        `json:"upload_id"`
	Parts      []S3CompleteMultipartPartItem `xml:"Part" json:"parts"`
}

type S3CompleteMultipartPartItem struct {
	PartNumber int32  `xml:"PartNumber" json:"part_number"`
	ETag       string `xml:"ETag" json:"etag"`
}

type S3CompleteMultipartUploadResp struct {
	XMLName  xml.Name `xml:"CompleteMultipartUploadResult" json:"-"`
	Location string   `xml:"Location" json:"location"`
	Bucket   string   `xml:"Bucket" json:"bucket"`
	Key      string   `xml:"Key" json:"key"`
	ETag     string   `xml:"ETag" json:"etag"`
}

type S3AbortMultipartUploadReq struct {
	BucketName string `json:"bucket_name"`
	ObjectKey  string `json:"object_key"`
	UploadID   string `json:"upload_id"`
}
