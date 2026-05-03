package policy

import (
	"context"
	"strings"
	"time"

	"oss/adaptor"
	bucketRepo "oss/adaptor/repo/bucket"
	policyRepo "oss/adaptor/repo/policy"
	"oss/common"
	"oss/service/do"
	"oss/service/dto"
)

type Service struct {
	repo       policyRepo.IPolicyRepo
	bucketRepo bucketRepo.IBucketRepo
}

func NewService(adaptor adaptor.IAdaptor) *Service {
	return &Service{
		repo:       policyRepo.NewPolicyRepo(adaptor),
		bucketRepo: bucketRepo.NewBucketRepo(adaptor),
	}
}

func (srv *Service) CreateBucketPolicy(ctx context.Context, bucketName string, req *dto.CreateBucketPolicyReq) (*dto.CreateBucketPolicyResp, common.Errno) {
	if strings.TrimSpace(bucketName) == "" {
		return nil, common.ParamErr.WithMsg("bucket_name is required")
	}
	if strings.TrimSpace(req.Name) == "" {
		return nil, common.ParamErr.WithMsg("name is required")
	}
	if len(req.Principals) == 0 {
		return nil, common.ParamErr.WithMsg("principals is required")
	}
	if len(req.Actions) == 0 {
		return nil, common.ParamErr.WithMsg("actions is required")
	}
	if len(req.Resources) == 0 {
		return nil, common.ParamErr.WithMsg("resources is required")
	}

	if req.Effect == "" {
		req.Effect = "Allow"
	} else if req.Effect != "Allow" && req.Effect != "Deny" {
		return nil, common.ParamErr.WithMsg("effect must be Allow or Deny")
	}

	bucketDo, err := srv.bucketRepo.GetByName(ctx, bucketName)
	if err != nil {
		return nil, common.ParamErr.WithErr(err)
	}

	principals := make([]*do.PolicyPrincipalDo, 0, len(req.Principals))
	for _, principal := range req.Principals {
		if strings.TrimSpace(principal.Type) == "" || strings.TrimSpace(principal.Value) == "" {
			return nil, common.ParamErr.WithMsg("principals must include type and value")
		}
		principals = append(principals, &do.PolicyPrincipalDo{Type: principal.Type, Value: principal.Value})
	}

	conditions := make([]*do.PolicyConditionDo, 0, len(req.Conditions))
	for _, condition := range req.Conditions {
		if strings.TrimSpace(condition.Type) == "" || strings.TrimSpace(condition.Value) == "" {
			return nil, common.ParamErr.WithMsg("condition type and value are required")
		}
		conditions = append(conditions, &do.PolicyConditionDo{Type: condition.Type, CondKey: condition.CondKey, Value: condition.Value})
	}

	policyID, err := srv.repo.CreateBucketPolicy(ctx, bucketDo.ID, &do.CreateBucketPolicy{
		Effect:     req.Effect,
		Name:       req.Name,
		Status:     1,
		Principals: principals,
		Actions:    req.Actions,
		Resources:  req.Resources,
		Conditions: conditions,
	})
	if err != nil {
		return nil, common.DatabaseErr.WithErr(err)
	}

	now := time.Now().UnixMilli()
	return &dto.CreateBucketPolicyResp{
		PolicyID:  policyID,
		Name:      req.Name,
		Status:    1,
		BucketID:  bucketDo.ID,
		CreatedAt: now,
		UpdatedAt: now,
	}, common.OK
}

func (srv *Service) ListBucketPolicies(ctx context.Context, bucketName string) (*dto.ListBucketPoliciesResp, common.Errno) {
	if strings.TrimSpace(bucketName) == "" {
		return nil, common.ParamErr.WithMsg("bucket_name is required")
	}

	bucketDo, err := srv.bucketRepo.GetByName(ctx, bucketName)
	if err != nil {
		return nil, common.ParamErr.WithErr(err)
	}

	policies, err := srv.repo.ListBucketPolicies(ctx, bucketDo.ID)
	if err != nil {
		return nil, common.DatabaseErr.WithErr(err)
	}

	items := make([]*dto.BucketPolicyItem, 0, len(policies))
	for _, policy := range policies {
		principalItems := make([]dto.PolicyPrincipalItem, 0, len(policy.Principals))
		for _, p := range policy.Principals {
			principalItems = append(principalItems, dto.PolicyPrincipalItem{Type: p.Type, Value: p.Value})
		}

		conditionItems := make([]dto.PolicyConditionItem, 0, len(policy.Conditions))
		for _, cond := range policy.Conditions {
			conditionItems = append(conditionItems, dto.PolicyConditionItem{Type: cond.Type, CondKey: cond.CondKey, Value: cond.Value})
		}

		items = append(items, &dto.BucketPolicyItem{
			PolicyID:   policy.ID,
			BucketID:   policy.BucketID,
			Effect:     policy.Effect,
			Name:       policy.Name,
			Status:     policy.Status,
			Principals: principalItems,
			Actions:    policy.Actions,
			Resources:  policy.Resources,
			Conditions: conditionItems,
			CreatedAt:  policy.CreatedAt.UnixMilli(),
			UpdatedAt:  policy.UpdatedAt.UnixMilli(),
		})
	}

	return &dto.ListBucketPoliciesResp{Items: items}, common.OK
}
