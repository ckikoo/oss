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

	"github.com/samber/lo"
	"gorm.io/gorm"
)

type LifecycleRepo struct {
	db *gorm.DB
	q  *query.Query
}

var _ lifecycle.ILifecycleRepo = (*LifecycleRepo)(nil)

func NewLifecycleRepo(db *gorm.DB) *LifecycleRepo {
	return &LifecycleRepo{db: db, q: query.Use(db)}
}

func (r *LifecycleRepo) WithTx(tx tx.Tx) lifecycle.ILifecycleRepo {
	return &LifecycleRepo{db: tx.(*gorm.DB), q: query.Use(tx.(*gorm.DB))}
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

	if err := r.q.LifecycleRule.WithContext(ctx).Create(&model); err != nil {
		return 0, repoerr.Wrap(err)
	}
	return model.ID, nil
}

func (r *LifecycleRepo) ListLifecycleRules(ctx context.Context, bucketID int64) ([]*do.LifecycleRuleDo, error) {
	modelRules, err := r.q.LifecycleRule.WithContext(ctx).Where(r.q.LifecycleRule.BucketID.Eq(bucketID)).Order(r.q.LifecycleRule.ID.Desc()).Find()
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
	q := r.q.LifecycleRule

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
	modelRule, err := r.q.LifecycleRule.WithContext(ctx).Where(r.q.LifecycleRule.BucketID.Eq(bucketID), r.q.LifecycleRule.ID.Eq(ruleID)).First()
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, repoerr.ErrNotFound
		}
		return nil, repoerr.Wrap(err)
	}
	return r.toLifecycleRuleDo(modelRule), nil
}

func (r *LifecycleRepo) UpdateLifecycleRule(ctx context.Context, bucketID, ruleID int64, update *do.UpdateLifecycleRule) (*do.LifecycleRuleDo, error) {
	qs := r.q.LifecycleRule.WithContext(ctx)
	updates := map[string]interface{}{}
	if update.RuleName != nil {
		updates[r.q.LifecycleRule.RuleName.ColumnName().String()] = *update.RuleName
	}
	if update.Prefix != nil {
		updates[r.q.LifecycleRule.Prefix.ColumnName().String()] = update.Prefix
	}
	if update.TransitionDays != nil {
		updates[r.q.LifecycleRule.TransitionDays.ColumnName().String()] = *update.TransitionDays
	}
	if update.TransitionStorageClass != nil {
		updates[r.q.LifecycleRule.TransitionStorageClass.ColumnName().String()] = *update.TransitionStorageClass
	}
	if update.ExpirationDays != nil {
		updates[r.q.LifecycleRule.ExpirationDays.ColumnName().String()] = *update.ExpirationDays
	}
	if update.Status != nil {
		updates[r.q.LifecycleRule.Status.ColumnName().String()] = *update.Status
	}
	if len(updates) == 0 {
		return nil, repoerr.Wrap(errors.New("no update fields"))
	}
	updates[r.q.LifecycleRule.UpdatedAt.ColumnName().String()] = time.Now()

	if _, err := qs.Where(r.q.LifecycleRule.BucketID.Eq(bucketID), r.q.LifecycleRule.ID.Eq(ruleID)).Updates(updates); err != nil {
		return nil, repoerr.Wrap(err)
	}

	return r.GetLifecycleRule(ctx, bucketID, ruleID)
}

func (r *LifecycleRepo) DeleteLifecycleRule(ctx context.Context, bucketID int64, ruleIDs ...int64) error {
	ruleIDs = lo.Union(ruleIDs)
	if bucketID <= 0 || len(ruleIDs) == 0 {
		return nil
	}

	q := r.q.LifecycleRule
	_, err := q.WithContext(ctx).
		Where(q.BucketID.Eq(bucketID), q.ID.In(ruleIDs...)).
		Delete()
	return repoerr.Wrap(err)
}
