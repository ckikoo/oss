package multipart

import (
	"sort"

	"oss/common"
	"oss/service/do"
)

func (srv *Service) ListParts(ctx *common.UserInfoCtx, uploadID string) (*do.MultipartUploadDo, []*do.MultipartPartDo, common.Errno) {
	upload, err := srv.multipartRepo.GetMultipartUploadByID(ctx, ctx.UserID, uploadID)
	if err != nil {
		return nil, nil, common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.FileUploadIdNotFound)
	}

	parts, err := srv.multipartRepo.ListMultipartParts(ctx, ctx.UserID, uploadID)
	if err != nil {
		return nil, nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}
	sort.Slice(parts, func(i, j int) bool {
		return parts[i].PartNumber < parts[j].PartNumber
	})

	return upload, parts, common.OK
}
