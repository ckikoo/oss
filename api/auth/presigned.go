package auth

// type PresignedCtrl struct {
// 	presigned *presigned.Service
// }

// func NewPresignedCtrl(service *presigned.Service) *PresignedCtrl {
// 	return &PresignedCtrl{presigned: service}
// }

// func (ctrl *PresignedCtrl) CreatePresignedUrl(ctx context.Context, c *app.RequestContext) {

// 	ak := c.GetString(consts.AccessKeyContext)
// 	sk := c.GetString(consts.SecretKeyContext)

// 	req := &dto.CreatePresignedUrlReq{}
// 	if err := c.BindAndValidate(req); err != nil {
// 		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
// 		return
// 	}

// 	resp, errno := ctrl.presigned.CreatePresignedUrl(ctx, ak, sk, req)
// 	api.WriteResp(c, resp, errno)
// }

// 创建下载url
// func (ctrl *PresignedCtrl) CreateDownloadURL(ctx context.Context, c *app.RequestContext) {
// 	ak := c.GetString(consts.AccessKeyContext)
// 	sk := c.GetString(consts.SecretKeyContext)

// 	req := &dto.CreateDownloadURLReq{}
// 	if err := c.BindAndValidate(req); err != nil {
// 		api.WriteResp(c, nil, common.ParamErr.WithErr(err))
// 		return
// 	}

// 	resp, errno := ctrl.presigned.CreateDownloadURL(ctx, ak, sk, req)
// 	api.WriteResp(c, resp, errno)
// }

// // 创建简单上传URL
// func (ctrl *PresignedCtrl) CreateUploadURL(ctx context.Context, c *app.RequestContext) {
// 	ak := c.GetString(consts.AccessKeyContext)
// 	sk := c.GetString(consts.SecretKeyContext)
// }

// // 分片上传URL
// func (ctrl *PresignedCtrl) CreateMultipartUploadURL(ctx context.Context, c *app.RequestContext) {
// 	ak := c.GetString(consts.AccessKeyContext)
// 	sk := c.GetString(consts.SecretKeyContext)
// }
