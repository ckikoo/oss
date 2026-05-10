package event

import (
	"context"
	"oss/adaptor/repo/model"
	"oss/adaptor/repo/query"
	"oss/service/do"

	"gorm.io/gorm"
)

type eventRuleRepo struct {
	db *gorm.DB
}

func NewEventRuleRepo(db *gorm.DB) IEventRuleRepo {
	return &eventRuleRepo{db: db}
}

func (r *eventRuleRepo) CreateEventRule(ctx context.Context, rule *do.EventRuleDo) (int64, error) {
	q := query.Use(r.db).EventRule
	model := &model.EventRule{
		BucketID:   rule.BucketID,
		RuleName:   rule.RuleName,
		Events:     rule.Events,
		Prefix:     rule.Prefix,
		Suffix:     rule.Suffix,
		TargetType: rule.TargetType,
		TargetURL:  rule.TargetURL,
		Secret:     rule.Secret,
		Status:     rule.Status,
	}

	err := q.WithContext(ctx).Create(model)
	if err != nil {
		return 0, err
	}

	return model.ID, nil
}

func (r *eventRuleRepo) GetByID(ctx context.Context, ruleID int64) (*do.EventRuleDo, error) {

	q := query.Use(r.db).EventRule

	model, err := q.WithContext(ctx).Where(q.ID.Eq(ruleID)).First()
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	return &do.EventRuleDo{
		ID:         model.ID,
		BucketID:   model.BucketID,
		RuleName:   model.RuleName,
		Events:     model.Events,
		Prefix:     model.Prefix,
		Suffix:     model.Suffix,
		TargetType: model.TargetType,
		TargetURL:  model.TargetURL,
		Secret:     model.Secret,
		Status:     model.Status,
		CreatedAt:  model.CreatedAt,
		UpdatedAt:  model.UpdatedAt,
	}, nil
}

func (r *eventRuleRepo) GetByBucketIDAndRuleName(ctx context.Context, bucketID int64, ruleName string) (*do.EventRuleDo, error) {
	q := query.Use(r.db).EventRule
	model, err := q.WithContext(ctx).Where(q.BucketID.Eq(bucketID), (q.RuleName.Eq(ruleName))).First()
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}

	return &do.EventRuleDo{
		ID:         model.ID,
		BucketID:   model.BucketID,
		RuleName:   model.RuleName,
		Events:     model.Events,
		Prefix:     model.Prefix,
		Suffix:     model.Suffix,
		TargetType: model.TargetType,
		TargetURL:  model.TargetURL,
		Secret:     model.Secret,
		Status:     model.Status,
		CreatedAt:  model.CreatedAt,
		UpdatedAt:  model.UpdatedAt,
	}, nil
}

func (r *eventRuleRepo) ListByBucketID(ctx context.Context, bucketID int64) ([]*do.EventRuleDo, error) {
	q := query.Use(r.db).EventRule
	list, err := q.WithContext(ctx).Where(q.BucketID.Eq(bucketID)).Order(q.CreatedAt.Desc()).Find()
	if err != nil {
		return nil, err
	}

	rules := make([]*do.EventRuleDo, 0, len(list))
	for _, model := range list {
		rules = append(rules, &do.EventRuleDo{
			ID:         model.ID,
			BucketID:   model.BucketID,
			RuleName:   model.RuleName,
			Events:     model.Events,
			Prefix:     model.Prefix,
			Suffix:     model.Suffix,
			TargetType: model.TargetType,
			TargetURL:  model.TargetURL,
			Secret:     model.Secret,
			Status:     model.Status,
			CreatedAt:  model.CreatedAt,
			UpdatedAt:  model.UpdatedAt,
		})
	}

	return rules, nil
}

func (r *eventRuleRepo) ListActiveRulesByBucketID(ctx context.Context, bucketID int64) ([]*do.EventRuleDo, error) {
	q := query.Use(r.db).EventRule

	models, err := q.WithContext(ctx).Where(q.BucketID.Eq(bucketID), (q.Status.Eq(1))).Find()
	if err != nil {
		return nil, err
	}

	rules := make([]*do.EventRuleDo, 0, len(models))
	for _, model := range models {
		rules = append(rules, &do.EventRuleDo{
			ID:         model.ID,
			BucketID:   model.BucketID,
			RuleName:   model.RuleName,
			Events:     model.Events,
			Prefix:     model.Prefix,
			Suffix:     model.Suffix,
			TargetType: model.TargetType,
			TargetURL:  model.TargetURL,
			Secret:     model.Secret,
			Status:     model.Status,
			CreatedAt:  model.CreatedAt,
			UpdatedAt:  model.UpdatedAt,
		})
	}

	return rules, nil
}

func (r *eventRuleRepo) UpdateEventRule(ctx context.Context, ruleID int64, update *do.UpdateEventRule) error {
	q := query.Use(r.db).EventRule
	model := make(map[string]interface{})

	if update.RuleName != nil {
		model[q.RuleName.ColumnName().String()] = *update.RuleName
	}
	if update.Events != nil {
		model[q.Events.ColumnName().String()] = *update.Events
	}
	if update.Prefix != nil {
		model[q.Prefix.ColumnName().String()] = update.Prefix
	}
	if update.Suffix != nil {
		model[q.Suffix.ColumnName().String()] = update.Suffix
	}
	if update.TargetType != nil {
		model[q.TargetType.ColumnName().String()] = *update.TargetType
	}
	if update.TargetURL != nil {
		model[q.TargetURL.ColumnName().String()] = update.TargetURL
	}
	if update.Secret != nil {
		model[q.Secret.ColumnName().String()] = update.Secret
	}
	if update.Status != nil {
		model[q.Status.ColumnName().String()] = *update.Status
	}

	_, err := q.WithContext(ctx).Where(q.ID.Eq(ruleID)).Updates(model)
	if err != nil {
		return err
	}
	return nil
}

func (r *eventRuleRepo) DeleteEventRule(ctx context.Context, ruleID int64) error {
	q := query.Use(r.db).EventRule

	_, err := q.WithContext(ctx).Where(q.ID.Eq(ruleID)).Delete()
	if err != nil {
		return err
	}
	return nil
}
