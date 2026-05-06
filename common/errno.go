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
	OK                   = Errno{Code: 200, Msg: "OK"}
	ServerErr            = Errno{Code: 500, Msg: "Internal Server Error"}
	ParamErr             = Errno{Code: 400, Msg: "Param Error"}
	AuthErr              = Errno{Code: 401, Msg: "Auth Error"}
	PermissionErr        = Errno{Code: 403, Msg: "Permission Error"}
	ResouceNotFoundErr   = Errno{Code: 404, Msg: "Permission Error"}
	DatabaseErr          = Errno{Code: 10000, Msg: "Database Error"}
	RedisErr             = Errno{Code: 10001, Msg: "Redis Error"}
	FileCheckErr         = Errno{Code: 11000, Msg: "文件校验错误"}
	FilePartyErr         = Errno{Code: 11001, Msg: "文件分片不一致"}
	FileNameExists       = Errno{Code: 11002, Msg: "文件名称已经存在"}
	FileHasUploadSuccess = Errno{Code: 11006, Msg: "该上传id 已经合并"}
	FilePartNotFound     = Errno{Code: 11003, Msg: "分片不存在"}
	TokenInvalid         = Errno{Code: 11004, Msg: "Token无效"}
	TokenExpired         = Errno{Code: 11005, Msg: "Token已过期"}
	StorageQuotaOver     = Errno{Code: 12000, Msg: "剩余空间不够"}
	BucketNotFoundErr    = Errno{Code: 13000, Msg: "Bucket Not Found"}
)
