package policy

import (
	"context"

	"oss/consts"
	"oss/service/do"
)

func (s *Service) Evaluate(ctx context.Context, req do.EvaluateReq) consts.Effect {
	policies, err := s.repo.ListPoliciesWithSubTablesByBucketID(ctx, req.BucketID)
	if err != nil || len(policies) == 0 {
		return consts.EffectNotApplicable
	}

	hasAllow := false

	for _, p := range policies {
		if p.Status != 1 {
			continue
		}
		if !matchPrincipal(p.Principals, req.Principals) {
			continue
		}
		if !matchAction(p.Actions, req.Action) {
			continue
		}
		if !matchResource(p.Resources, req.Resource) {
			continue
		}
		if !matchConditions(p.Conditions, req) {
			continue
		}

		if p.Effect == "Deny" {
			return consts.EffectDeny // Deny 优先，立即短路
		}
		hasAllow = true
	}

	if hasAllow {
		return consts.EffectAllow
	}
	return consts.EffectNotApplicable
}
