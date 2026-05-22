package gorm

import (
	"context"
	"encoding/json"
	"time"

	"golang.org/x/sync/singleflight"

	"oss/adaptor"
	"oss/adaptor/repo/event"
	"oss/adaptor/repo/model"
	"oss/adaptor/repo/query"
	"oss/adaptor/repo/repocache"
	"oss/adaptor/repo/repoerr"
	"oss/adaptor/tx"
	"oss/consts"
	"oss/service/do"
	"oss/utils/cache"
	"oss/utils/logger"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type eventRuleRepo struct {
	db           *gorm.DB
	q            *query.Query
	rds          *redis.Client
	cacheManager cache.IManager
	g            *singleflight.Group
	cacheEnabled bool
}

var _ event.IEventRuleRepo = (*eventRuleRepo)(nil)

func NewEventRuleRepo(adaptor adaptor.IAdaptor) event.IEventRuleRepo {
	db := adaptor.GetGORM()
	return &eventRuleRepo{
		db:           db,
		q:            query.Use(db),
		rds:          adaptor.GetRedis(),
		cacheManager: adaptor.GetCache(),
		g:            &singleflight.Group{},
		cacheEnabled: true,
	}
}

func (r *eventRuleRepo) WithTx(tx tx.Tx) event.IEventRuleRepo {
	db := tx.(*gorm.DB)
	return &eventRuleRepo{
		db:           db,
		q:            query.Use(db),
		rds:          r.rds,
		cacheManager: r.cacheManager,
		g:            r.g,
		cacheEnabled: false,
	}
}

func (r *eventRuleRepo) toDo(modelRule *model.EventRule) (*do.EventRuleDo, error) {
	if modelRule == nil {
		return nil, repoerr.ErrNotFound
	}

	return &do.EventRuleDo{
		ID:         modelRule.ID,
		BucketID:   modelRule.BucketID,
		RuleName:   modelRule.RuleName,
		Events:     modelRule.Events,
		Prefix:     modelRule.Prefix,
		Suffix:     modelRule.Suffix,
		TargetType: modelRule.TargetType,
		TargetURL:  modelRule.TargetURL,
		Secret:     modelRule.Secret,
		Status:     modelRule.Status,
		CreatedAt:  modelRule.CreatedAt,
		UpdatedAt:  modelRule.UpdatedAt,
	}, nil
}

func (r *eventRuleRepo) toDos(models []*model.EventRule) ([]*do.EventRuleDo, error) {
	rules := make([]*do.EventRuleDo, 0, len(models))
	for _, modelRule := range models {
		rule, err := r.toDo(modelRule)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

func (r *eventRuleRepo) eventRulesCacheKey(bucketID int64) string {
	return consts.EventRulesCacheKey(bucketID)
}

func (r *eventRuleRepo) activeEventRulesCacheKey(bucketID int64) string {
	return consts.EventActiveRulesCacheKey(bucketID)
}

func (r *eventRuleRepo) eventRuleCacheKey(ruleID int64) string {
	return consts.EventRuleCacheKey(ruleID)
}

func (r *eventRuleRepo) getCachedRedisRule(ctx context.Context, key string) *do.EventRuleDo {
	if r.rds == nil {
		return nil
	}
	val, err := r.rds.Get(ctx, key).Result()
	if err != nil {
		return nil
	}

	var rule do.EventRuleDo
	if err := json.Unmarshal([]byte(val), &rule); err != nil {
		return nil
	}
	return &rule
}

func (r *eventRuleRepo) getCachedRedisRuleList(ctx context.Context, key string) []*do.EventRuleDo {
	if r.rds == nil {
		return nil
	}
	val, err := r.rds.Get(ctx, key).Result()
	if err != nil {
		return nil
	}

	var rules []*do.EventRuleDo
	if err := json.Unmarshal([]byte(val), &rules); err != nil {
		return nil
	}
	return rules
}

func (r *eventRuleRepo) setAllCaches(ctx context.Context, key string, value any) {
	if r.cacheManager == nil {
		return
	}
	r.cacheManager.Set(key, value, 0)

	data, err := json.Marshal(value)
	if err != nil {
		logger.GetLogger().Warn("failed to marshal event cache value",
			zap.Error(err),
			zap.String("key", key),
		)
		return
	}

	if err := r.rds.Set(ctx, key, data, time.Duration(consts.CacheTTLEventRule)*time.Second).Err(); err != nil {
		logger.GetLogger().Warn("failed to set event redis cache",
			zap.Error(err),
			zap.String("key", key),
		)
	}
}

func (r *eventRuleRepo) invalidateEventRuleCache(ctx context.Context, bucketID, ruleID int64) {
	keys := []string{
		r.eventRulesCacheKey(bucketID),
		r.activeEventRulesCacheKey(bucketID),
	}

	if ruleID > 0 {
		keys = append(keys, r.eventRuleCacheKey(ruleID))
	}

	repocache.Invalidator{
		RDS:     r.rds,
		Local:   r.cacheManager,
		LogName: "event",
	}.AfterCommit(ctx, keys...)
}

func (r *eventRuleRepo) getEventRuleByIDCache(ctx context.Context, ruleID int64) (*do.EventRuleDo, error) {
	cacheKey := r.eventRuleCacheKey(ruleID)
	return repocache.Accessor[*do.EventRuleDo]{
		RDS:     r.rds,
		Local:   r.cacheManager,
		Group:   r.g,
		TTL:     time.Duration(consts.CacheTTLEventRule) * time.Second,
		Enabled: r.cacheEnabled,
		LogName: "event-rule",
	}.Get(ctx, cacheKey, func(context.Context) (*do.EventRuleDo, error) {
		q := r.q.EventRule
		modelRule, err := q.WithContext(ctx).Where(q.ID.Eq(ruleID)).First()
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil, nil
			}
			return nil, repoerr.Wrap(err)
		}
		return r.toDo(modelRule)
	})

	if entry, ok := r.cacheManager.Get(cacheKey); ok {
		if rule, ok := entry.Data.(*do.EventRuleDo); ok {
			return rule, nil
		}
		r.cacheManager.Remove(cacheKey)
	}

	if cached := r.getCachedRedisRule(ctx, cacheKey); cached != nil {
		r.cacheManager.Set(cacheKey, cached, 0)
		return cached, nil
	}

	result, err, _ := r.g.Do(cacheKey, func() (interface{}, error) {
		if cached := r.getCachedRedisRule(ctx, cacheKey); cached != nil {
			return cached, nil
		}

		q := r.q.EventRule
		modelRule, err := q.WithContext(ctx).Where(q.ID.Eq(ruleID)).First()
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil, nil
			}
			return nil, repoerr.Wrap(err)
		}

		rule, err := r.toDo(modelRule)
		if err != nil {
			return nil, err
		}

		r.setAllCaches(ctx, cacheKey, rule)
		return rule, nil
	})
	if err != nil {
		return nil, err
	}

	if result == nil {
		return nil, nil
	}

	rule := result.(*do.EventRuleDo)
	r.cacheManager.Set(cacheKey, rule, 0)
	return rule, nil
}

func (r *eventRuleRepo) getActiveEventRulesCache(ctx context.Context, bucketID int64) ([]*do.EventRuleDo, error) {
	cacheKey := r.activeEventRulesCacheKey(bucketID)
	return repocache.Accessor[[]*do.EventRuleDo]{
		RDS:     r.rds,
		Local:   r.cacheManager,
		Group:   r.g,
		TTL:     time.Duration(consts.CacheTTLEventRule) * time.Second,
		Enabled: r.cacheEnabled,
		LogName: "event-active-rules",
	}.Get(ctx, cacheKey, func(context.Context) ([]*do.EventRuleDo, error) {
		q := r.q.EventRule
		models, err := q.WithContext(ctx).Where(q.BucketID.Eq(bucketID), q.Status.Eq(1)).Find()
		if err != nil {
			return nil, repoerr.Wrap(err)
		}
		return r.toDos(models)
	})

	if entry, ok := r.cacheManager.Get(cacheKey); ok {
		if rules, ok := entry.Data.([]*do.EventRuleDo); ok {
			return rules, nil
		}
		r.cacheManager.Remove(cacheKey)
	}

	if cached := r.getCachedRedisRuleList(ctx, cacheKey); cached != nil {
		r.cacheManager.Set(cacheKey, cached, 0)
		return cached, nil
	}

	result, err, _ := r.g.Do(cacheKey, func() (interface{}, error) {
		if cached := r.getCachedRedisRuleList(ctx, cacheKey); cached != nil {
			return cached, nil
		}

		q := r.q.EventRule
		models, err := q.WithContext(ctx).Where(q.BucketID.Eq(bucketID), q.Status.Eq(1)).Find()
		if err != nil {
			return nil, repoerr.Wrap(err)
		}

		rules := make([]*do.EventRuleDo, 0, len(models))
		for _, modelRule := range models {
			rule, err := r.toDo(modelRule)
			if err != nil {
				return nil, err
			}
			rules = append(rules, rule)
		}

		r.setAllCaches(ctx, cacheKey, rules)
		return rules, nil
	})
	if err != nil {
		return nil, err
	}

	if result == nil {
		return nil, nil
	}

	rules := result.([]*do.EventRuleDo)
	r.cacheManager.Set(cacheKey, rules, 0)
	return rules, nil
}

func (r *eventRuleRepo) getEventRulesByBucketCache(ctx context.Context, bucketID int64) ([]*do.EventRuleDo, error) {
	cacheKey := r.eventRulesCacheKey(bucketID)
	return repocache.Accessor[[]*do.EventRuleDo]{
		RDS:     r.rds,
		Local:   r.cacheManager,
		Group:   r.g,
		TTL:     time.Duration(consts.CacheTTLEventRule) * time.Second,
		Enabled: r.cacheEnabled,
		LogName: "event-rules",
	}.Get(ctx, cacheKey, func(context.Context) ([]*do.EventRuleDo, error) {
		q := r.q.EventRule
		models, err := q.WithContext(ctx).Where(q.BucketID.Eq(bucketID)).Order(q.CreatedAt.Desc()).Find()
		if err != nil {
			return nil, repoerr.Wrap(err)
		}
		return r.toDos(models)
	})

	if entry, ok := r.cacheManager.Get(cacheKey); ok {
		if rules, ok := entry.Data.([]*do.EventRuleDo); ok {
			return rules, nil
		}
		r.cacheManager.Remove(cacheKey)
	}

	if cached := r.getCachedRedisRuleList(ctx, cacheKey); cached != nil {
		r.cacheManager.Set(cacheKey, cached, 0)
		return cached, nil
	}

	result, err, _ := r.g.Do(cacheKey, func() (interface{}, error) {
		if cached := r.getCachedRedisRuleList(ctx, cacheKey); cached != nil {
			return cached, nil
		}

		q := r.q.EventRule
		models, err := q.WithContext(ctx).Where(q.BucketID.Eq(bucketID)).Order(q.CreatedAt.Desc()).Find()
		if err != nil {
			return nil, repoerr.Wrap(err)
		}

		rules := make([]*do.EventRuleDo, 0, len(models))
		for _, modelRule := range models {
			rule, err := r.toDo(modelRule)
			if err != nil {
				return nil, err
			}
			rules = append(rules, rule)
		}

		r.setAllCaches(ctx, cacheKey, rules)
		return rules, nil
	})
	if err != nil {
		return nil, err
	}

	if result == nil {
		return nil, nil
	}

	rules := result.([]*do.EventRuleDo)
	r.cacheManager.Set(cacheKey, rules, 0)
	return rules, nil
}

func (r *eventRuleRepo) CreateEventRule(ctx context.Context, rule *do.EventRuleDo) (int64, error) {
	q := r.q.EventRule
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
		return 0, repoerr.Wrap(err)
	}

	r.invalidateEventRuleCache(ctx, model.BucketID, model.ID)
	return model.ID, nil
}

func (r *eventRuleRepo) GetByID(ctx context.Context, ruleID int64) (*do.EventRuleDo, error) {
	return r.getEventRuleByIDCache(ctx, ruleID)
}

func (r *eventRuleRepo) GetByBucketIDAndRuleName(ctx context.Context, bucketID int64, ruleName string) (*do.EventRuleDo, error) {
	q := r.q.EventRule
	model, err := q.WithContext(ctx).Where(q.BucketID.Eq(bucketID), (q.RuleName.Eq(ruleName))).First()
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, repoerr.Wrap(err)
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
	return r.getEventRulesByBucketCache(ctx, bucketID)
}

func (r *eventRuleRepo) ListActiveRulesByBucketID(ctx context.Context, bucketID int64) ([]*do.EventRuleDo, error) {
	return r.getActiveEventRulesCache(ctx, bucketID)
}

func (r *eventRuleRepo) UpdateEventRule(ctx context.Context, ruleID int64, update *do.UpdateEventRule) error {
	oldRule, err := r.GetByID(ctx, ruleID)
	if err != nil {
		return err
	}
	if oldRule == nil {
		return repoerr.ErrNotFound
	}

	q := r.q.EventRule
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

	_, err = q.WithContext(ctx).Where(q.ID.Eq(ruleID)).Updates(model)
	if err != nil {
		return repoerr.Wrap(err)
	}

	r.invalidateEventRuleCache(ctx, oldRule.BucketID, ruleID)
	return nil
}

func (r *eventRuleRepo) DeleteEventRule(ctx context.Context, ruleID int64) error {
	oldRule, err := r.GetByID(ctx, ruleID)
	if err != nil {
		return err
	}
	if oldRule == nil {
		return nil
	}

	q := r.q.EventRule
	_, err = q.WithContext(ctx).Where(q.ID.Eq(ruleID)).Delete()
	if err != nil {
		return repoerr.Wrap(err)
	}

	r.invalidateEventRuleCache(ctx, oldRule.BucketID, ruleID)
	return nil
}
