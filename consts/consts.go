package consts

const (
	UserKeyContext   = "user_id"
	AccessKeyContext = "access_key"
)

// 1=正常 2=禁用 3=注销
const (
	UserStatusEnable  = 1
	UserStatusDisable = 2
	UserStatusDeleted = 3
)

const (
	AccessKeyStatusEnable  = 1
	AccessKeyStatusDisable = 2
)

const (
	BucketAclPrivate    = 0
	BucketAclPublicRead = 1
	BucketAclPublicRW   = 2
)

const (
	ObjectAclInheritBucket = 0
	ObjectAclPrivate       = 1
	ObjectAclPublicRead    = 2
)

const (
	FilePermDir  = 0755
	FilePermFile = 0644
)

const (
	DefaultMaxKeys = 1000
)

const (
	BucketVersioningDisabled = 1
	BucketVersioningEnabled  = 2
)

const (
	BucketStatusNormal  = 1
	BucketStatusLocked  = 2
	BucketStatusDeleted = 3
)

const (
	ObjectStatusNormal     = 1
	ObjectStatusDeleteMark = 2
	ObjectStatusDeleted    = 3
)

const (
	ObjectIsMultipartNormal = 0
	ObjectIsMultipartMerged = 1
)

const (
	MultipartUploadStatusUploading      = 0
	MultipartUploadStatusMergedVirtual  = 1
	MultipartUploadStatusMergedPhysical = 2
	MultipartUploadStatusFailed         = 3
	MultipartUploadStatusAborted        = 4
)

const (
	OperationLogResultFailed  = 0
	OperationLogResultSuccess = 1
)

const (
	DateFormatYMD = "2006-01-02"
)

const (
	MultipartPartStatusUploading = 0
	MultipartPartStatusConfirmed = 1
	MultipartPartStatusMerged    = 2
)

const (
	StorageClassStandard = "STANDARD"
	StorageClassIA       = "IA"
	StorageClassArchive  = "ARCHIVE"
)
