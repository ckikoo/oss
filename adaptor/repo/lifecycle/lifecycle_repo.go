package lifecycle

import (
	"context"
	"errors"
	"time"

	"oss/adaptor"
	"oss/adaptor/repo/model"
	"oss/adaptor/repo/query"
	"oss/service/do"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type LifecycleRepo struct {
	db *gorm.DB
}

var _ ILifecycleRepo = (*LifecycleRepo)(nil)

func NewLifecycleRepo(adaptor adaptor.IAdaptor) *LifecycleRepo {
	sqlDB := adaptor.GetDB()
	ormDB, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		panic(err)
	}
	return &LifecycleRepo{db: ormDB}
}

func (r *LifecycleRepo) CreateLifecycleRule(ctx context.Context, rule *do.CreateLifecycleRule) (int64, error) {
	modelRule := &model.LifecycleRule{
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
		modelRule.AbortIncompleteMultipartDays = *rule.AbortIncompleteMultipartDays
	}

	if err := query.Use(r.db).LifecycleRule.WithContext(ctx).Create(modelRule); err != nil {
		return 0, err
	}
	return modelRule.ID, nil
}

func (r *LifecycleRepo) ListLifecycleRules(ctx context.Context, bucketID int64) ([]*do.LifecycleRuleDo, error) {
	modelRules, err := query.Use(r.db).LifecycleRule.WithContext(ctx).Where(query.Use(r.db).LifecycleRule.BucketID.Eq(bucketID)).Order(query.Use(r.db).LifecycleRule.ID.Desc()).Find()
	if err != nil {
		return nil, err
	}

	rules := make([]*do.LifecycleRuleDo, 0, len(modelRules))
	for _, m := range modelRules {
		rules = append(rules, &do.LifecycleRuleDo{
			ID:                              m.ID,
			BucketID:                        m.BucketID,
			RuleName:                        m.RuleName,
			Status:                          m.Status,
			Prefix:                          m.Prefix,
			TransitionDays:                  m.TransitionDays,
			TransitionStorageClass:          m.TransitionStorageClass,
			ExpirationDays:                  m.ExpirationDays,
			NoncurrentVersionExpirationDays: m.NoncurrentVersionExpirationDays,
			AbortIncompleteMultipartDays:    m.AbortIncompleteMultipartDays,
			CreatedAt:                       m.CreatedAt,
			UpdatedAt:                       m.UpdatedAt,
		})
	}
	return rules, nil
}

func (r *LifecycleRepo) ListAllActiveLifecycleRules(ctx context.Context) ([]*do.LifecycleRuleDo, error) {
	modelRules, err := query.Use(r.db).LifecycleRule.WithContext(ctx).Where(query.Use(r.db).LifecycleRule.Status.Eq(1)).Find()
	if err != nil {
		return nil, err
	}

	rules := make([]*do.LifecycleRuleDo, 0, len(modelRules))
	for _, modelRule := range modelRules {
		rules = append(rules, &do.LifecycleRuleDo{
			ID:                             modelRule.ID,
			BucketID:                       modelRule.BucketID,
			RuleName:                       modelRule.RuleName,
			Status:                         modelRule.Status,
			Prefix:                         modelRule.Prefix,
			TransitionDays:                 modelRule.TransitionDays,
			TransitionStorageClass:         modelRule.TransitionStorageClass,
			ExpirationDays:                 modelRule.ExpirationDays,
			NoncurrentVersionExpirationDays: modelRule.NoncurrentVersionExpirationDays,
			AbortIncompleteMultipartDays:   modelRule.AbortIncompleteMultipartDays,
			CreatedAt:                      modelRule.CreatedAt,
			UpdatedAt:                      modelRule.UpdatedAt,
		})
	}
	return rules, nil
}

func (r *LifecycleRepo) GetLifecycleRule(ctx context.Context, bucketID, ruleID int64) (*do.LifecycleRuleDo, error) {
	modelRule, err := query.Use(r.db).LifecycleRule.WithContext(ctx).Where(query.Use(r.db).LifecycleRule.BucketID.Eq(bucketID), query.Use(r.db).LifecycleRule.ID.Eq(ruleID)).First()
	if err != nil {
		return nil, err
	}
	if modelRule == nil {
		return nil, errors.New("lifecycle rule not found")
	}
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
	}, nil
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
		return nil, errors.New("no update fields")
	}
	updates[query.Use(r.db).LifecycleRule.UpdatedAt.ColumnName().String()] = time.Now()

	if _, err := qs.Where(query.Use(r.db).LifecycleRule.BucketID.Eq(bucketID), query.Use(r.db).LifecycleRule.ID.Eq(ruleID)).Updates(updates); err != nil {
		return nil, err
	}

	return r.GetLifecycleRule(ctx, bucketID, ruleID)
}

func (r *LifecycleRepo) DeleteLifecycleRule(ctx context.Context, bucketID, ruleID int64) error {
	modelRule, err := query.Use(r.db).LifecycleRule.WithContext(ctx).Where(query.Use(r.db).LifecycleRule.BucketID.Eq(bucketID), query.Use(r.db).LifecycleRule.ID.Eq(ruleID)).First()
	if err != nil {
		return err
	}
	if modelRule == nil {
		return errors.New("lifecycle rule not found")
	}
	_, err = query.Use(r.db).LifecycleRule.WithContext(ctx).Delete(modelRule)
	return err
}
