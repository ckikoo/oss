package s3

import (
	"io"

	"oss/common"
	"oss/service/dto"
)

type IS3Service interface {
	ListBuckets(ctx *common.UserInfoCtx) (*dto.S3ListBucketsResp, common.Errno)
	CreateBucket(ctx *common.UserInfoCtx, req *dto.S3CreateBucketReq) (*dto.S3CreateBucketResp, common.Errno)
	HeadBucket(ctx *common.UserInfoCtx, bucketName string) common.Errno
	GetBucketLocation(ctx *common.UserInfoCtx, bucketName string) (*dto.S3GetBucketLocationResp, common.Errno)
	DeleteBucket(ctx *common.UserInfoCtx, bucketName string) common.Errno

	ListObjectsV2(ctx *common.UserInfoCtx, req *dto.S3ListObjectsV2Req) (*dto.S3ListObjectsV2Resp, common.Errno)
	PutObject(ctx *common.UserInfoCtx, req *dto.S3PutObjectReq, body io.Reader) (*dto.S3PutObjectResp, common.Errno)
	GetObject(ctx *common.UserInfoCtx, req *dto.S3GetObjectReq) (*dto.S3GetObjectResp, common.Errno)
	HeadObject(ctx *common.UserInfoCtx, req *dto.S3HeadObjectReq) (*dto.S3HeadObjectResp, common.Errno)
	DeleteObject(ctx *common.UserInfoCtx, req *dto.S3DeleteObjectReq) common.Errno
	DeleteObjects(ctx *common.UserInfoCtx, req *dto.S3DeleteObjectsReq) (*dto.S3DeleteObjectsResp, common.Errno)
	CopyObject(ctx *common.UserInfoCtx, req *dto.S3CopyObjectReq) (*dto.S3CopyObjectResp, common.Errno)

	CreateMultipartUpload(ctx *common.UserInfoCtx, req *dto.S3CreateMultipartUploadReq) (*dto.S3CreateMultipartUploadResp, common.Errno)
	UploadPart(ctx *common.UserInfoCtx, req *dto.S3UploadPartReq, body io.Reader) (*dto.S3UploadPartResp, common.Errno)
	ListParts(ctx *common.UserInfoCtx, req *dto.S3ListPartsReq) (*dto.S3ListPartsResp, common.Errno)
	CompleteMultipartUpload(ctx *common.UserInfoCtx, req *dto.S3CompleteMultipartUploadReq) (*dto.S3CompleteMultipartUploadResp, common.Errno)
	AbortMultipartUpload(ctx *common.UserInfoCtx, req *dto.S3AbortMultipartUploadReq) common.Errno
}
