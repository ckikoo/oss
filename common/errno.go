package common

type Errno struct {
	Code   int
	Msg    string
	ErrMsg string
}

func (err Errno) Error() string {
	return err.Msg
}

func (err Errno) WithMsg(msg string) Errno {
	err.Msg = err.Msg + "," + msg
	return err
}

func (err Errno) WithErr(rawErr error) Errno {
	var msg string
	if rawErr != nil {
		msg = rawErr.Error()
	}
	err.ErrMsg = err.Msg + "," + msg
	return err
}

func (err Errno) IsOk() bool {
	return err.Code == 200
}

func (err Errno) NotOk() bool {
	return !err.IsOk()
}

var (
	OK                            = Errno{Code: 200, Msg: "OK"}
	ServerErr                     = Errno{Code: 500, Msg: "Internal Server Error"}
	ParamErr                      = Errno{Code: 400, Msg: "Param Error"}
	AuthErr                       = Errno{Code: 401, Msg: "Auth Error"}
	PermissionErr                 = Errno{Code: 403, Msg: "Permission Error"}
	ResouceNotFoundErr            = Errno{Code: 404, Msg: "Resource Not Found"}
	ReadBodyError                 = Errno{Code: 20000, Msg: "read body error"}
	DatabaseErr                   = Errno{Code: 10000, Msg: "Database Error"}
	RedisErr                      = Errno{Code: 10001, Msg: "Redis Error"}
	FileCheckErr                  = Errno{Code: 11000, Msg: "文件校验错误"}
	FilePartyErr                  = Errno{Code: 11001, Msg: "文件分片不一致"}
	FileNameExists                = Errno{Code: 11002, Msg: "文件名称已经存在"}
	FilePartNotFound              = Errno{Code: 11003, Msg: "分片不存在"}
	FilePartSizeOutLimit          = Errno{Code: 11004, Msg: "分片大小超过限制"}
	FilePartNumError              = Errno{Code: 11005, Msg: "分片数量错误"}
	FileHasUploadSuccess          = Errno{Code: 11006, Msg: "该上传id 已经合并"}
	FileUploadIdNotFound          = Errno{Code: 11006, Msg: "uploadId 不存在或者已经失效"}
	FileUploadIdStatusNotOnUpload = Errno{Code: 11006, Msg: "uploadId 状态不在上传中"}
	TokenInvalid                  = Errno{Code: 15000, Msg: "Token无效"}
	TokenExpired                  = Errno{Code: 15001, Msg: "Token已过期"}
	StorageQuotaOver              = Errno{Code: 12000, Msg: "剩余空间不够"}
	BucketNotFoundErr             = Errno{Code: 13000, Msg: "Bucket Not Found"}
	BucketNotEmptyErr             = Errno{Code: 13001, Msg: "Bucket is not empty"}
	EventRuleAlreadyExists        = Errno{Code: 14000, Msg: "Event Rule Already Exists"}
	EventRuleNotFound             = Errno{Code: 14001, Msg: "Event Rule Not Found"}
	ErrInvalidParams              = Errno{Code: 400, Msg: "Invalid Parameters"}
	ErrInternalServer             = Errno{Code: 500, Msg: "Internal Server Error"}
	ConflictErr                   = Errno{Code: 409, Msg: "Conflict"}
)
