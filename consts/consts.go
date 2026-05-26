package consts

const (
	UserKeyContext   = "user_id"
	UserInfoContext  = "user_info"
	AccessKeyContext = "access_key"
	SecretKeyContext = "secret_key"
	BucketContext    = "bucket"
	TokenGranted     = "token_granted"
)

const MaxPartSize = 10 << 20

const (
	// 使用签名算法
	ServerName = "oss-server"
)

// 1=正常 2=禁用 3=注销
const (
	UserStatusEnable  = 1
	UserStatusDisable = 2
	UserStatusDeleted = 3
)

const (
	AccessKeyStatusEnable  = 0
	AccessKeyStatusDisable = 1
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
	ObjectIsDirNo  = 0
	ObjectIsDirYes = 1
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
	MultipartPartStatusUploading    = 0
	MultipartPartStatusConfirmed    = 1
	MultipartPartStatusVirtualMerge = 2
)

const (
	StorageClassStandard = "STANDARD"
	StorageClassIA       = "IA"
	StorageClassArchive  = "ARCHIVE"
)

// ValidStorageClass 验证存储类是否有效
func ValidStorageClass(storageClass string) bool {
	switch storageClass {
	case StorageClassStandard, StorageClassIA, StorageClassArchive:
		return true
	default:
		return false
	}
}

const (
	DownloadAction = "download"
	DownloadMethod = "GET"

	UploadAction = "upload"
	UploadMethod = "PUT"
)

const (
	HeaderToken = "X-OSS-Token"
)

const (
	TaskTypePhysicalMerge       = "PHYSICAL_MERGE"
	TaskTypePhysicalCopy        = "PHYSICAL_COPY"
	TaskTypeTranscode           = "TRANSCODE"
	TaskTypeImageProcess        = "IMG_PROCESS"
	TaskTypeAbortMultipart      = "ABORT_MULTIPART"
	TaskTypeLifecycleTransition = "LIFECYCLE_TRANSITION"
	TaskTypeLifecycleExpiration = "LIFECYCLE_EXPIRATION"
)

const (
	TaskBizTypeUpload = "upload"
)

const (
	TaskStatusPending   int32 = 0 // 已写入 DB，等待扫描入队
	TaskStatusQueued    int32 = 1 // 已写入 Redis LIST，等待 worker 消费
	TaskStatusRunning   int32 = 2 // worker 已取走并执行中
	TaskStatusCompleted int32 = 3 // 完成
	TaskStatusFailed    int32 = 4 // 重试耗尽后失败
	TaskStatusCanceled  int32 = 5 // 用户取消
	TaskStatusDead      int32 = 6 // 死信，需要人工介入
)

func ValidAsyncTaskType(taskType string) bool {
	switch taskType {
	case TaskTypePhysicalMerge, TaskTypeTranscode, TaskTypeImageProcess, TaskTypeAbortMultipart, TaskTypeLifecycleTransition, TaskTypeLifecycleExpiration:
		return true
	default:
		return false
	}
}

func ValidAsyncTaskStatus(status int32) bool {
	switch status {
	case TaskStatusPending, TaskStatusQueued, TaskStatusRunning, TaskStatusCompleted, TaskStatusFailed, TaskStatusCanceled, TaskStatusDead:
		return true
	default:
		return false
	}
}

// 事件相关常量
const (
	EventRuleStatusEnabled  = 1
	EventRuleStatusDisabled = 0
)

const (
	EventDeliveryStatusPending    = 0
	EventDeliveryStatusSuccess    = 1
	EventDeliveryStatusFailed     = 2
	EventDeliveryStatusProcessing = 3
)

const (
	EventTypePutObject           = "PutObject"
	EventTypeGetObject           = "GetObject"
	EventTypeDeleteObject        = "DeleteObject"
	EventTypeMultipartComplete   = "MultipartComplete"
	EventTypeLifecycleTransition = "LifecycleTransition"
	EventTypeLifecycleExpiration = "LifecycleExpiration"
)

const (
	EventTargetTypeWebhook = "WEBHOOK"
	EventTargetTypeMQ      = "MQ"
	EventTargetTypeRedis   = "REDIS"
	EventTargetTypeFunc    = "FUNC"
)
