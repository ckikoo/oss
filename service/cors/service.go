package cors

import (
	"context"
	"strings"

	"oss/adaptor"
	bucketrepo "oss/adaptor/repo/bucket"
	bucketgorm "oss/adaptor/repo/bucket/gorm"
	corsrepo "oss/adaptor/repo/cors"
	corsgorm "oss/adaptor/repo/cors/gorm"
	"oss/adaptor/repo/repoerr"
	"oss/common"
	"oss/service/do"
	"oss/service/dto"
	"oss/utils/logger"

	"go.uber.org/zap"
)

type Service struct {
	corsRepo   corsrepo.IBucketCorsRepo
	bucketRepo bucketrepo.IBucketRepo
	logger     *zap.Logger
}

var allBucketCorsMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}

func NewService(adaptor adaptor.IAdaptor) *Service {
	return &Service{
		corsRepo:   corsgorm.NewBucketCorsRepo(adaptor),
		bucketRepo: bucketgorm.NewBucketRepo(adaptor),
		logger:     logger.GetLogger().With(zap.String("module", "cors")),
	}
}

func (srv *Service) CreateBucketCorsRule(ctx *common.UserInfoCtx, bucketName string, req *dto.CreateBucketCorsRuleReq) (*dto.BucketCorsRuleResp, common.Errno) {
	if errno := srv.validateBucketOwner(ctx, bucketName); errno.NotOk() {
		return nil, errno
	}

	origin, methods, errno := validateRuleValues(req.Origin, req.AllowedMethods, req.MaxAgeSeconds)
	if errno.NotOk() {
		return nil, errno
	}

	rule, err := srv.corsRepo.Create(ctx, &do.CreateBucketCorsRule{
		UserID:         ctx.UserID,
		BucketName:     bucketName,
		Origin:         origin,
		AllowedMethods: methods,
		MaxAgeSeconds:  normalizeMaxAge(req.MaxAgeSeconds),
	})
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	return toRuleResp(rule), common.OK
}

func (srv *Service) ListBucketCorsRules(ctx *common.UserInfoCtx, bucketName string) (*dto.ListBucketCorsRulesResp, common.Errno) {
	if errno := srv.validateBucketOwner(ctx, bucketName); errno.NotOk() {
		return nil, errno
	}

	rules, err := srv.corsRepo.ListByBucket(ctx, ctx.UserID, bucketName)
	if err != nil {
		return nil, common.ErrnoFromRepoError(err, common.DatabaseErr)
	}

	items := make([]*dto.BucketCorsRuleResp, 0, len(rules))
	for _, rule := range rules {
		items = append(items, toRuleResp(rule))
	}

	return &dto.ListBucketCorsRulesResp{Items: items}, common.OK
}

func (srv *Service) UpdateBucketCorsRule(ctx *common.UserInfoCtx, bucketName string, ruleID int64, req *dto.UpdateBucketCorsRuleReq) (*dto.BucketCorsRuleResp, common.Errno) {
	if errno := srv.validateBucketOwner(ctx, bucketName); errno.NotOk() {
		return nil, errno
	}

	update := &do.UpdateBucketCorsRule{}
	if req.Origin != nil {
		origin, errno := normalizeOrigin(*req.Origin)
		if errno.NotOk() {
			return nil, errno
		}
		update.Origin = &origin
	}
	if len(req.AllowedMethods) > 0 {
		methods, errno := normalizeMethods(req.AllowedMethods)
		if errno.NotOk() {
			return nil, errno
		}
		update.AllowedMethods = methods
	}
	if req.MaxAgeSeconds != nil {
		if *req.MaxAgeSeconds < 0 {
			return nil, common.ParamErr.WithMsg("max_age_seconds must be greater than or equal to 0")
		}
		maxAge := normalizeMaxAge(*req.MaxAgeSeconds)
		update.MaxAgeSeconds = &maxAge
	}

	rule, err := srv.corsRepo.Update(ctx, ctx.UserID, bucketName, ruleID, update)
	if err != nil {
		return nil, common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.ResouceNotFoundErr)
	}

	return toRuleResp(rule), common.OK
}

func (srv *Service) DeleteBucketCorsRule(ctx *common.UserInfoCtx, bucketName string, ruleID int64) common.Errno {
	if errno := srv.validateBucketOwner(ctx, bucketName); errno.NotOk() {
		return errno
	}

	if err := srv.corsRepo.Delete(ctx, ctx.UserID, bucketName, ruleID); err != nil {
		return common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.ResouceNotFoundErr)
	}

	return common.OK
}

func (srv *Service) CheckBucketCors(ctx context.Context, userID int64, bucketName, origin, omethod string) (*dto.BucketCorsCheckResult, common.Errno) {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return &dto.BucketCorsCheckResult{}, common.OK
	}
	bucketName = strings.TrimSpace(bucketName)
	if bucketName == "" {
		return nil, common.ParamErr.WithMsg("bucket_name is required")
	}
	if userID <= 0 {
		return nil, common.AuthErr.WithMsg("cors requires authenticated user context")
	}

	omethod = strings.ToUpper(strings.TrimSpace(omethod))
	if omethod == "" {
		return nil, common.ParamErr.WithMsg("method is required")
	}

	rule, err := srv.corsRepo.GetMatchedRule(ctx, userID, bucketName, origin)
	if err != nil && err != repoerr.ErrNotFound {
		srv.logger.Error("cors check: get matched bucket cors rule failed",
			zap.Error(err),
			zap.Int64("user_id", userID),
			zap.String("bucket_name", bucketName),
			zap.String("origin", origin),
		)
		return nil, common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.PermissionErr.WithMsg("bucket cors rule denied"))
	}

	// 未建立跨域请求，允许任何源
	if rule == nil {
		return &dto.BucketCorsCheckResult{
			AllowedOrigin:  "*",
			AllowedMethods: []string{omethod},
			MaxAgeSeconds:  60,
		}, common.OK
	}

	hasPass := false
	for _, method := range rule.AllowedMethods {
		if strings.EqualFold(strings.TrimSpace(method), omethod) {
			hasPass = true
			break
		}
	}

	if !hasPass {
		return nil, common.PermissionErr.WithMsg("bucket cors rule denied")
	}

	return &dto.BucketCorsCheckResult{
		AllowedOrigin:  allowedOriginValue(rule.Origin, origin),
		AllowedMethods: rule.AllowedMethods,
		MaxAgeSeconds:  rule.MaxAgeSeconds,
	}, common.OK
}

func (srv *Service) validateBucketOwner(ctx *common.UserInfoCtx, bucketName string) common.Errno {
	if strings.TrimSpace(bucketName) == "" {
		return common.ParamErr.WithMsg("bucket_name is required")
	}

	bucket, err := srv.bucketRepo.GetByUserAndName(ctx, ctx.UserID, bucketName)
	if err != nil {
		return common.ErrnoFromRepoErrorWithNotFound(err, common.DatabaseErr, common.BucketNotFoundErr)
	}
	if bucket == nil {
		return common.BucketNotFoundErr
	}

	return common.OK
}

func validateRuleValues(origin string, methods []string, maxAge int32) (string, []string, common.Errno) {
	if maxAge < 0 {
		return "", nil, common.ParamErr.WithMsg("max_age_seconds must be greater than or equal to 0")
	}

	normalizedOrigin, errno := normalizeOrigin(origin)
	if errno.NotOk() {
		return "", nil, errno
	}

	normalizedMethods, errno := normalizeMethods(methods)
	if errno.NotOk() {
		return "", nil, errno
	}

	return normalizedOrigin, normalizedMethods, common.OK
}

func normalizeOrigin(origin string) (string, common.Errno) {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return "", common.ParamErr.WithMsg("origin is required")
	}

	return origin, common.OK
}

func normalizeMethods(methods []string) ([]string, common.Errno) {
	if len(methods) == 0 {
		return nil, common.ParamErr.WithMsg("allowed_methods is required")
	}

	result := make([]string, 0, len(methods))
	seen := make(map[string]struct{}, len(methods))
	for _, method := range methods {
		method = strings.ToUpper(strings.TrimSpace(method))
		if method == "" {
			return nil, common.ParamErr.WithMsg("allowed_methods contains empty value")
		}
		if method == "*" {
			for _, expanded := range allBucketCorsMethods {
				if _, ok := seen[expanded]; ok {
					continue
				}
				seen[expanded] = struct{}{}
				result = append(result, expanded)
			}
			continue
		}
		if _, ok := seen[method]; ok {
			continue
		}
		seen[method] = struct{}{}
		result = append(result, method)
	}

	return result, common.OK
}

func normalizeMaxAge(maxAge int32) int32 {
	if maxAge <= 0 {
		return 600
	}
	return maxAge
}

func allowedOriginValue(allowedOrigin string, origin string) string {
	return origin
}

func toRuleResp(rule *do.BucketCorsRuleDo) *dto.BucketCorsRuleResp {
	return &dto.BucketCorsRuleResp{
		ID:             rule.ID,
		UserID:         rule.UserID,
		BucketName:     rule.BucketName,
		Origin:         rule.Origin,
		AllowedMethods: rule.AllowedMethods,
		MaxAgeSeconds:  rule.MaxAgeSeconds,
		CreatedAt:      rule.CreatedAt.UnixMilli(),
		UpdatedAt:      rule.UpdatedAt.UnixMilli(),
	}
}
