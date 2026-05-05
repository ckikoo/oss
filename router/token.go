package router

import (
	"oss/consts"
)

/*
* TODO
* 校验token的中间件， 主要校验生成的临时token， 针对某些接口
*
 */
var needTokenCheck = map[string]string{

	// 初始化分片上传
	"POST /buckets/:b/objects/:k/uploads": consts.UploadAction,
	// 每片
	"PUT /buckets/:b/objects/:k/uploads/:id/:n": consts.UploadAction,
	// 完成分片上传
	"POST /buckets/:b/objects/:k/uploads/:id": consts.UploadAction,
	// 完成合并
	"DELETE /buckets/:b/objects/:k/uploads/:id": consts.UploadAction,
	// 简单上传
	"PUT /buckets/:b/objects/:k/upload": consts.UploadAction,
}
