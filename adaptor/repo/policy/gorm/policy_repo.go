package admin

import (
	"context"
	"sync"
	"time"

	"oss/adaptor/repo/model"
	"oss/adaptor/repo/policy"
	"oss/adaptor/repo/query"
	"oss/service/do"
	"oss/utils/pool"

	"gorm.io/gorm"
)

type PolicyRepo struct {
	db *gorm.DB
}

var _ policy.IPolicyRepo = (*PolicyRepo)(nil)

func NewPolicyRepo(db *gorm.DB) *PolicyRepo {
	return &PolicyRepo{db: db}
}

func (r *PolicyRepo) CreateBucketPolicy(ctx context.Context, bucketID int64, policy *do.CreateBucketPolicy) (int64, error) {
	q := query.Use(r.db)
	var policyID int64
	err := q.Transaction(func(tx *query.Query) error {
		modelPolicy := &model.BucketPolicy{
			BucketID:  bucketID,
			Effect:    policy.Effect,
			Status:    policy.Status,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if policy.Name != "" {
			modelPolicy.Name = &policy.Name
		}

		if err := tx.BucketPolicy.WithContext(ctx).Create(modelPolicy); err != nil {
			return err
		}
		policyID = modelPolicy.ID

		if len(policy.Principals) > 0 {
			principalModels := make([]*model.PolicyPrincipal, 0, len(policy.Principals))
			for _, principal := range policy.Principals {
				principalModels = append(principalModels, &model.PolicyPrincipal{
					PolicyID: policyID,
					Type:     principal.Type,
					Value:    principal.Value,
				})
			}
			if err := tx.PolicyPrincipal.WithContext(ctx).Create(principalModels...); err != nil {
				return err
			}
		}

		if len(policy.Actions) > 0 {
			actionModels := make([]*model.PolicyAction, 0, len(policy.Actions))
			for _, action := range policy.Actions {
				actionModels = append(actionModels, &model.PolicyAction{
					PolicyID: policyID,
					Action:   action,
				})
			}
			if err := tx.PolicyAction.WithContext(ctx).Create(actionModels...); err != nil {
				return err
			}
		}

		if len(policy.Resources) > 0 {
			resourceModels := make([]*model.PolicyResource, 0, len(policy.Resources))
			for _, resource := range policy.Resources {
				resourceModels = append(resourceModels, &model.PolicyResource{
					PolicyID: policyID,
					Resource: resource,
				})
			}
			if err := tx.PolicyResource.WithContext(ctx).Create(resourceModels...); err != nil {
				return err
			}
		}

		if len(policy.Conditions) > 0 {
			conditionModels := make([]*model.PolicyCondition, 0, len(policy.Conditions))
			for _, condition := range policy.Conditions {
				conditionModels = append(conditionModels, &model.PolicyCondition{
					PolicyID: policyID,
					Type:     condition.Type,
					CondKey:  condition.CondKey,
					Value:    condition.Value,
				})
			}
			if err := tx.PolicyCondition.WithContext(ctx).Create(conditionModels...); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return policyID, nil
}

func (r *PolicyRepo) ListBucketPolicies(ctx context.Context, bucketID int64) ([]*do.BucketPolicyDo, error) {
	q := query.Use(r.db)
	modelPolicies, err := q.BucketPolicy.WithContext(ctx).Where(q.BucketPolicy.BucketID.Eq(bucketID)).Order(q.BucketPolicy.ID.Desc()).Find()
	if err != nil {
		return nil, err
	}

	policies := make([]*do.BucketPolicyDo, len(modelPolicies))
	if len(modelPolicies) == 0 {
		return policies, nil
	}

	poolSize := len(modelPolicies)
	if poolSize > 8 {
		poolSize = 8
	}
	p := pool.NewPoolWithSize(poolSize)
	defer p.Release()

	var firstErr error
	var errOnce sync.Once

	setErr := func(err error) {
		errOnce.Do(func() {
			firstErr = err
		})
	}

	for idx, modelPolicy := range modelPolicies {
		idx := idx
		modelPolicy := modelPolicy
		if err := p.RunGo(func() {
			localQ := query.Use(r.db)

			principals, err := localQ.PolicyPrincipal.WithContext(ctx).Where(localQ.PolicyPrincipal.PolicyID.Eq(modelPolicy.ID)).Find()
			if err != nil {
				setErr(err)
				return
			}

			actions, err := localQ.PolicyAction.WithContext(ctx).Where(localQ.PolicyAction.PolicyID.Eq(modelPolicy.ID)).Find()
			if err != nil {
				setErr(err)
				return
			}

			resources, err := localQ.PolicyResource.WithContext(ctx).Where(localQ.PolicyResource.PolicyID.Eq(modelPolicy.ID)).Find()
			if err != nil {
				setErr(err)
				return
			}

			conditions, err := localQ.PolicyCondition.WithContext(ctx).Where(localQ.PolicyCondition.PolicyID.Eq(modelPolicy.ID)).Find()
			if err != nil {
				setErr(err)
				return
			}

			principalItems := make([]*do.PolicyPrincipalDo, 0, len(principals))
			for _, p := range principals {
				principalItems = append(principalItems, &do.PolicyPrincipalDo{Type: p.Type, Value: p.Value})
			}

			actionItems := make([]string, 0, len(actions))
			for _, action := range actions {
				actionItems = append(actionItems, action.Action)
			}

			resourceItems := make([]string, 0, len(resources))
			for _, resource := range resources {
				resourceItems = append(resourceItems, resource.Resource)
			}

			conditionItems := make([]*do.PolicyConditionDo, 0, len(conditions))
			for _, condition := range conditions {
				conditionItems = append(conditionItems, &do.PolicyConditionDo{Type: condition.Type, CondKey: condition.CondKey, Value: condition.Value})
			}

			name := ""
			if modelPolicy.Name != nil {
				name = *modelPolicy.Name
			}

			policies[idx] = &do.BucketPolicyDo{
				ID:         modelPolicy.ID,
				BucketID:   modelPolicy.BucketID,
				Effect:     modelPolicy.Effect,
				Name:       name,
				Status:     modelPolicy.Status,
				CreatedAt:  modelPolicy.CreatedAt,
				UpdatedAt:  modelPolicy.UpdatedAt,
				Principals: principalItems,
				Actions:    actionItems,
				Resources:  resourceItems,
				Conditions: conditionItems,
			}
		}); err != nil {
			setErr(err)
		}
	}

	p.Wait()
	if firstErr != nil {
		return nil, firstErr
	}

	return policies, nil
}
