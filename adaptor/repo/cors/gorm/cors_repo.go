package gorm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"

	"oss/adaptor"
	corsrepo "oss/adaptor/repo/cors"
	"oss/adaptor/repo/model"
	"oss/adaptor/repo/query"
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

type BucketCorsRepo struct {
	db           *gorm.DB
	q            *query.Query
	rds          *redis.Client
	cacheManager cache.IManager
	g            *singleflight.Group
}

var _ corsrepo.IBucketCorsRepo = (*BucketCorsRepo)(nil)

func NewBucketCorsRepo(adaptor adaptor.IAdaptor) *BucketCorsRepo {
	return &BucketCorsRepo{
		q:            query.Use(adaptor.GetGORM()),
		db:           adaptor.GetGORM(),
		rds:          adaptor.GetRedis(),
		cacheManager: adaptor.GetCache(),
		g:            &singleflight.Group{},
	}
}

func (r *BucketCorsRepo) WithTx(tx tx.Tx) corsrepo.IBucketCorsRepo {
	return &BucketCorsRepo{
		q:            query.Use(tx.(*gorm.DB)),
		db:           tx.(*gorm.DB),
		rds:          r.rds,
		cacheManager: r.cacheManager,
		g:            r.g,
	}
}

func (r *BucketCorsRepo) toModel(rule *do.CreateBucketCorsRule) (*model.BucketCorsRule, error) {
	origin := strings.TrimSpace(rule.Origin)
	if origin == "" || len(rule.AllowedMethods) == 0 {
		return nil, repoerr.ErrInvalidData
	}

	maxAge := rule.MaxAgeSeconds
	if maxAge <= 0 {
		maxAge = 600
	}

	now := time.Now()
	return &model.BucketCorsRule{
		UserID:         rule.UserID,
		BucketName:     rule.BucketName,
		AllowedOrigin:  origin,
		AllowedMethods: methodsToDB(rule.AllowedMethods),
		MaxAgeSeconds:  maxAge,
		Enabled:        1,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

func (r *BucketCorsRepo) toDo(modelRule *model.BucketCorsRule) (*do.BucketCorsRuleDo, error) {
	if modelRule == nil {
		return nil, repoerr.ErrNotFound
	}

	methods, err := methodsFromDB(modelRule.AllowedMethods)
	if err != nil {
		return nil, err
	}

	maxAge := int32(600)
	if modelRule.MaxAgeSeconds > 0 {
		maxAge = modelRule.MaxAgeSeconds
	}

	return &do.BucketCorsRuleDo{
		ID:             modelRule.ID,
		UserID:         modelRule.UserID,
		BucketName:     modelRule.BucketName,
		Origin:         modelRule.AllowedOrigin,
		AllowedMethods: methods,
		MaxAgeSeconds:  maxAge,
		CreatedAt:      modelRule.CreatedAt,
		UpdatedAt:      modelRule.UpdatedAt,
	}, nil
}

func (r *BucketCorsRepo) Create(ctx context.Context, rule *do.CreateBucketCorsRule) (*do.BucketCorsRuleDo, error) {
	modelRule, err := r.toModel(rule)
	if err != nil {
		return nil, err
	}

	q := r.q.BucketCorsRule
	existingRule, err := q.WithContext(ctx).
		Where(q.UserID.Eq(modelRule.UserID), q.BucketName.Eq(modelRule.BucketName), q.AllowedOrigin.Eq(modelRule.AllowedOrigin)).
		First()
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, repoerr.Wrap(err)
		}
	} else {
		if existingRule.Enabled == 1 {
			return nil, repoerr.ErrDuplicate
		}

		updates := map[string]interface{}{
			q.AllowedMethods.ColumnName().String(): modelRule.AllowedMethods,
			q.MaxAgeSeconds.ColumnName().String():  modelRule.MaxAgeSeconds,
			q.Enabled.ColumnName().String():        int32(1),
			q.UpdatedAt.ColumnName().String():      time.Now(),
		}

		result, err := q.WithContext(ctx).
			Where(q.ID.Eq(existingRule.ID)).
			Updates(updates)
		if err != nil {
			return nil, repoerr.Wrap(err)
		}
		if result.RowsAffected == 0 {
			return nil, repoerr.ErrNotFound
		}

		existingRule.AllowedMethods = modelRule.AllowedMethods
		existingRule.MaxAgeSeconds = modelRule.MaxAgeSeconds
		existingRule.Enabled = 1
		existingRule.UpdatedAt = time.Now()

		r.invalidateBucketCorsCache(ctx, rule.UserID, rule.BucketName, rule.Origin)
		return r.toDo(existingRule)
	}

	if err := q.WithContext(ctx).Create(modelRule); err != nil {
		return nil, repoerr.Wrap(err)
	}

	r.invalidateBucketCorsCache(ctx, rule.UserID, rule.BucketName, rule.Origin)
	return r.toDo(modelRule)
}

func (r *BucketCorsRepo) ListByBucket(ctx context.Context, userID int64, bucketName string) ([]*do.BucketCorsRuleDo, error) {
	q := r.q.BucketCorsRule
	modelRules, err := q.WithContext(ctx).
		Where(q.UserID.Eq(userID), q.BucketName.Eq(bucketName), q.Enabled.Eq(1)).
		Order(q.ID.Desc()).
		Find()
	if err != nil {
		return nil, repoerr.Wrap(err)
	}

	rules := make([]*do.BucketCorsRuleDo, 0, len(modelRules))
	for _, modelRule := range modelRules {
		rule, err := r.toDo(modelRule)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}

	return rules, nil
}

func (r *BucketCorsRepo) GetMatchedRule(ctx context.Context, userID int64, bucketName, origin string) (*do.BucketCorsRuleDo, error) {
	origin = strings.TrimSpace(origin)
	bucketName = strings.TrimSpace(bucketName)
	if userID <= 0 || bucketName == "" || origin == "" {
		return nil, repoerr.ErrInvalidData
	}

	cacheKey := consts.BucketCorsOriginCacheKey(userID, bucketName, origin)
	return r.getRuleByKey(ctx, cacheKey, func() (*do.BucketCorsRuleDo, error) {
		return r.getMatchedRuleDB(ctx, userID, bucketName, origin)
	})
}

func (r *BucketCorsRepo) getMatchedRuleDB(ctx context.Context, userID int64, bucketName, origin string) (*do.BucketCorsRuleDo, error) {
	q := r.q.BucketCorsRule
	modelRule, err := q.WithContext(ctx).
		Where(q.UserID.Eq(userID), q.BucketName.Eq(bucketName), q.AllowedOrigin.Eq(origin), q.Enabled.Eq(1)).
		First()
	if err != nil {
		return nil, repoerr.Wrap(err)
	}
	return r.toDo(modelRule)
}

func (r *BucketCorsRepo) GetByID(ctx context.Context, userID int64, bucketName string, ruleID int64) (*do.BucketCorsRuleDo, error) {
	q := r.q.BucketCorsRule
	modelRule, err := q.WithContext(ctx).
		Where(q.UserID.Eq(userID), q.BucketName.Eq(bucketName), q.ID.Eq(ruleID), q.Enabled.Eq(1)).
		First()
	if err != nil {
		return nil, repoerr.Wrap(err)
	}

	return r.toDo(modelRule)
}

func (r *BucketCorsRepo) Update(ctx context.Context, userID int64, bucketName string, ruleID int64, update *do.UpdateBucketCorsRule) (*do.BucketCorsRuleDo, error) {
	q := r.q.BucketCorsRule
	updates := map[string]interface{}{}

	if update.Origin != nil {
		origin := strings.TrimSpace(*update.Origin)
		if origin == "" {
			return nil, repoerr.ErrInvalidData
		}
		updates[q.AllowedOrigin.ColumnName().String()] = origin
	}

	if len(update.AllowedMethods) > 0 {
		updates[q.AllowedMethods.ColumnName().String()] = methodsToDB(update.AllowedMethods)
	}

	if update.MaxAgeSeconds != nil {
		updates[q.MaxAgeSeconds.ColumnName().String()] = *update.MaxAgeSeconds
	}

	if len(updates) == 0 {
		return nil, repoerr.ErrInvalidData
	}

	oldRule, err := r.GetByID(ctx, userID, bucketName, ruleID)
	if err != nil {
		return nil, err
	}

	updates[q.UpdatedAt.ColumnName().String()] = time.Now()

	result, err := q.WithContext(ctx).
		Where(q.UserID.Eq(userID), q.BucketName.Eq(bucketName), q.ID.Eq(ruleID)).
		Updates(updates)
	if err != nil {
		return nil, repoerr.Wrap(err)
	}
	if result.RowsAffected == 0 {
		return nil, repoerr.ErrNotFound
	}

	origins := []string{oldRule.Origin}
	if update.Origin != nil {
		newOrigin := strings.TrimSpace(*update.Origin)
		if !strings.EqualFold(oldRule.Origin, newOrigin) {
			origins = append(origins, newOrigin)
		}
	}
	r.invalidateBucketCorsCache(ctx, userID, bucketName, origins...)
	return r.GetByID(ctx, userID, bucketName, ruleID)
}

func (r *BucketCorsRepo) Delete(ctx context.Context, userID int64, bucketName string, ruleID int64) error {
	q := r.q.BucketCorsRule
	modelRule, err := q.WithContext(ctx).
		Where(q.UserID.Eq(userID), q.BucketName.Eq(bucketName), q.ID.Eq(ruleID), q.Enabled.Eq(1)).
		First()
	if err != nil {
		return repoerr.Wrap(err)
	}

	result, err := q.WithContext(ctx).
		Where(q.UserID.Eq(userID), q.BucketName.Eq(bucketName), q.ID.Eq(ruleID), q.Enabled.Eq(1)).
		Updates(map[string]interface{}{
			q.Enabled.ColumnName().String():   int32(0),
			q.UpdatedAt.ColumnName().String(): time.Now(),
		})
	if err != nil {
		return repoerr.Wrap(err)
	}
	if result.RowsAffected == 0 {
		return repoerr.ErrNotFound
	}

	r.invalidateBucketCorsCache(ctx, userID, bucketName, modelRule.AllowedOrigin)
	return nil
}

func (r *BucketCorsRepo) getCachedRedis(ctx context.Context, key string) *do.BucketCorsRuleDo {
	val, err := r.rds.Get(ctx, key).Result()
	if err != nil {
		return nil
	}

	var rule do.BucketCorsRuleDo
	if err := json.Unmarshal([]byte(val), &rule); err != nil {
		return nil
	}
	return &rule
}

func (r *BucketCorsRepo) setCachedRedis(ctx context.Context, key string, rule *do.BucketCorsRuleDo) error {
	data, err := json.Marshal(rule)
	if err != nil {
		return repoerr.Wrap(err)
	}
	return repoerr.Wrap(r.rds.Set(ctx, key, data, time.Duration(consts.CacheTTLBucketCors)*time.Second).Err())
}

func (r *BucketCorsRepo) setAllCaches(ctx context.Context, key string, rule *do.BucketCorsRuleDo) {
	r.cacheManager.Set(key, rule, 0)
	if err := r.setCachedRedis(ctx, key, rule); err != nil {
		logger.Warn("failed to set bucket cors redis cache",
			zap.Error(err),
			zap.String("key", key),
		)
	}
}

func (r *BucketCorsRepo) invalidateBucketCorsCache(ctx context.Context, userID int64, bucketName string, origins ...string) {
	keys := make([]string, 0, len(origins))
	for _, origin := range origins {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}
		keys = append(keys, consts.BucketCorsOriginCacheKey(userID, bucketName, origin))
	}
	if len(keys) == 0 {
		return
	}

	r.rds.Del(ctx, keys...)
	r.cacheManager.Remove(keys...)

	if err := r.cacheManager.Publish(ctx, keys...); err != nil {
		logger.Warn("failed to publish bucket cors cache invalidation",
			zap.Error(err),
			zap.Strings("keys", keys),
		)
	}
}

func (r *BucketCorsRepo) getRuleByKey(ctx context.Context, cacheKey string, queryFn func() (*do.BucketCorsRuleDo, error)) (*do.BucketCorsRuleDo, error) {
	if entry, ok := r.cacheManager.Get(cacheKey); ok {
		if rule, ok := entry.Data.(*do.BucketCorsRuleDo); ok {
			return rule, nil
		}
		r.cacheManager.Remove(cacheKey)
	}

	if cached := r.getCachedRedis(ctx, cacheKey); cached != nil {
		r.cacheManager.Set(cacheKey, cached, 0)
		return cached, nil
	}

	result, err, _ := r.g.Do(cacheKey, func() (interface{}, error) {
		if cached := r.getCachedRedis(ctx, cacheKey); cached != nil {
			return cached, nil
		}

		rule, err := queryFn()
		if err != nil {
			return nil, err
		}

		r.setAllCaches(ctx, cacheKey, rule)
		return rule, nil
	})
	if err != nil {
		return nil, err
	}

	rule := result.(*do.BucketCorsRuleDo)
	r.cacheManager.Set(cacheKey, rule, 0)
	return rule, nil
}

func methodsToDB(methods []string) string {
	return strings.Join(methods, ",")
}

func methodsFromDB(raw string) ([]string, error) {
	parts := strings.Split(raw, ",")
	methods := make([]string, 0, len(parts))
	for _, part := range parts {
		method := strings.ToUpper(strings.TrimSpace(part))
		if method != "" {
			methods = append(methods, method)
		}
	}
	if len(methods) == 0 {
		return nil, repoerr.ErrInvalidData
	}
	return methods, nil
}
