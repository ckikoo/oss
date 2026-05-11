package gorm

import (
	"context"
	"errors"
	"time"

	"oss/adaptor/repo/lifecycle"
	"oss/adaptor/repo/model"
	"oss/adaptor/repo/query"
	"oss/adaptor/repo/repoerr"
	"oss/adaptor/tx"
	"oss/service/do"

	"gorm.io/gorm"
)

type LifecycleRepo struct {
	db *gorm.DB
}

var _ lifecycle.ILifecycleRepo = (*LifecycleRepo)(nil)

func NewLifecycleRepo(db *gorm.DB) *LifecycleRepo {
	return &LifecycleRepo{db: db}
}

func (r *LifecycleRepo) WithTx(tx tx.Tx) lifecycle.ILifecycleRepo {
	return &LifecycleRepo{db: tx.(*gorm.DB)}
}

func (r *LifecycleRepo) toLifecycleRuleDo(modelRule *model.LifecycleRule) *do.LifecycleRuleDo {
	return &do.LifecycleRuleDo{
		ID:                              modelRule.ID,
		BucketID:                        modelRule.BucketID,
		RuleName:                        modelRule.RuleName,
		Status:                          modelRule.Status,
		Prefix:                          modelRule.Prefix,
		TransitionDays:                  modelRule.TransitionDays,
		TransitionStorageClass:          modelRule.TransitionStorageClass,
		ExpirationDays:                  modelRule.ExpirationDays,
		NoncurrentVersionExpirationDays: modelRule.NoncurrentVersionExpirationDays,
		AbortIncompleteMultipartDays:    modelRule.AbortIncompleteMultipartDays,
		CreatedAt:                       modelRule.CreatedAt,
		UpdatedAt:                       modelRule.UpdatedAt,
	}
}
func (r *LifecycleRepo) CreateLifecycleRule(ctx context.Context, rule *do.CreateLifecycleRule) (int64, error) {

	model := model.LifecycleRule{
		BucketID:                        rule.BucketID,
		RuleName:                        rule.RuleName,
		Status:                          rule.Status,
		Prefix:                          rule.Prefix,
		TransitionDays:                  rule.TransitionDays,
		TransitionStorageClass:          rule.TransitionStorageClass,
		ExpirationDays:                  rule.ExpirationDays,
		NoncurrentVersionExpirationDays: rule.NoncurrentVersionExpirationDays,
		AbortIncompleteMultipartDays:    7,
		CreatedAt:                       time.Now(),
		UpdatedAt:                       time.Now(),
	}

	if rule.AbortIncompleteMultipartDays != nil {
		model.AbortIncompleteMultipartDays = *rule.AbortIncompleteMultipartDays
	}

	if err := query.Use(r.db).LifecycleRule.WithContext(ctx).Create(&model); err != nil {
		return 0, repoerr.Wrap(err)
	}
	return model.ID, nil
}

func (r *LifecycleRepo) ListLifecycleRules(ctx context.Context, bucketID int64) ([]*do.LifecycleRuleDo, error) {
	modelRules, err := query.Use(r.db).LifecycleRule.WithContext(ctx).Where(query.Use(r.db).LifecycleRule.BucketID.Eq(bucketID)).Order(query.Use(r.db).LifecycleRule.ID.Desc()).Find()
	if err != nil {
		return nil, repoerr.Wrap(err)
	}

	rules := make([]*do.LifecycleRuleDo, 0, len(modelRules))
	for _, m := range modelRules {
		rules = append(rules, r.toLifecycleRuleDo(m))
	}
	return rules, nil
}

func (r *LifecycleRepo) ListAllActiveLifecycleRulesByCursor(ctx context.Context, cursor int64, limit int) ([]*do.LifecycleRuleDo, error) {
	q := query.Use(r.db).LifecycleRule

	modelRules, err := q.WithContext(ctx).
		Where(q.Status.Eq(1), q.ID.Gt(cursor)). // ID > cursor
		Order(q.ID.Asc()).
		Limit(limit).
		Find()
	if err != nil {
		return nil, repoerr.Wrap(err)
	}

	rules := make([]*do.LifecycleRuleDo, 0, len(modelRules))
	for _, modelRule := range modelRules {
		rules = append(rules, r.toLifecycleRuleDo(modelRule))
	}
	return rules, nil
}

func (r *LifecycleRepo) GetLifecycleRule(ctx context.Context, bucketID, ruleID int64) (*do.LifecycleRuleDo, error) {
	modelRule, err := query.Use(r.db).LifecycleRule.WithContext(ctx).Where(query.Use(r.db).LifecycleRule.BucketID.Eq(bucketID), query.Use(r.db).LifecycleRule.ID.Eq(ruleID)).First()
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, repoerr.ErrNotFound
		}
		return nil, repoerr.Wrap(err)
	}
	return r.toLifecycleRuleDo(modelRule), nil
}

func (r *LifecycleRepo) UpdateLifecycleRule(ctx context.Context, bucketID, ruleID int64, update *do.UpdateLifecycleRule) (*do.LifecycleRuleDo, error) {
	qs := query.Use(r.db).LifecycleRule.WithContext(ctx)
	updates := map[string]interface{}{}
	if update.RuleName != nil {
		updates[query.Use(r.db).LifecycleRule.RuleName.ColumnName().String()] = *update.RuleName
	}
	if update.Prefix != nil {
		updates[query.Use(r.db).LifecycleRule.Prefix.ColumnName().String()] = update.Prefix
	}
	if update.TransitionDays != nil {
		updates[query.Use(r.db).LifecycleRule.TransitionDays.ColumnName().String()] = *update.TransitionDays
	}
	if update.TransitionStorageClass != nil {
		updates[query.Use(r.db).LifecycleRule.TransitionStorageClass.ColumnName().String()] = *update.TransitionStorageClass
	}
	if update.ExpirationDays != nil {
		updates[query.Use(r.db).LifecycleRule.ExpirationDays.ColumnName().String()] = *update.ExpirationDays
	}
	if update.Status != nil {
		updates[query.Use(r.db).LifecycleRule.Status.ColumnName().String()] = *update.Status
	}
	if len(updates) == 0 {
		return nil, repoerr.Wrap(errors.New("no update fields"))
	}
	updates[query.Use(r.db).LifecycleRule.UpdatedAt.ColumnName().String()] = time.Now()

	if _, err := qs.Where(query.Use(r.db).LifecycleRule.BucketID.Eq(bucketID), query.Use(r.db).LifecycleRule.ID.Eq(ruleID)).Updates(updates); err != nil {
		return nil, repoerr.Wrap(err)
	}

	return r.GetLifecycleRule(ctx, bucketID, ruleID)
}

func (r *LifecycleRepo) DeleteLifecycleRule(ctx context.Context, bucketID, ruleID int64) error {
	modelRule, err := query.Use(r.db).LifecycleRule.WithContext(ctx).Where(query.Use(r.db).LifecycleRule.BucketID.Eq(bucketID), query.Use(r.db).LifecycleRule.ID.Eq(ruleID)).First()
	if err != nil {
		return repoerr.Wrap(err)
	}
	if modelRule == nil {
		return repoerr.Wrap(errors.New("lifecycle rule not found"))
	}
	_, err = query.Use(r.db).LifecycleRule.WithContext(ctx).Delete(modelRule)
	return repoerr.Wrap(err)
}
