package gorm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/gogf/gf/util/gconv"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"

	"oss/adaptor"
	"oss/adaptor/repo/admin"
	"oss/adaptor/repo/model"
	"oss/adaptor/repo/query"
	"oss/adaptor/repo/repoerr"
	"oss/adaptor/tx"
	"oss/consts"
	"oss/service/do"
	"oss/utils/cache"
	"oss/utils/logger"
)

type User struct {
	db           *gorm.DB
	q            *query.Query
	rds          *redis.Client
	cacheManager cache.IManager
	g            *singleflight.Group
}

var _ admin.IUser = (*User)(nil)

func NewUserRepo(a adaptor.IAdaptor) *User {
	return &User{
		db:           a.GetGORM(),
		q:            query.Use(a.GetGORM()),
		rds:          a.GetRedis(),
		g:            &singleflight.Group{},
		cacheManager: a.GetCache(),
	}
}

func (u *User) WithTx(tx tx.Tx) admin.IUser {
	txDB, _ := tx.(*gorm.DB)
	return &User{
		db:           txDB,
		q:            query.Use(txDB),
		rds:          u.rds,
		cacheManager: u.cacheManager,
		g:            u.g,
	}
}

func (u *User) CreateUser(ctx context.Context, req *do.CreateUser) (int64, error) {
	qs := u.q.User

	timeNow := time.Now()

	user := &model.User{
		Email:        req.Email,
		Status:       consts.UserStatusEnable,
		StorageQuota: req.StorageQuota,
		CreatedAt:    timeNow,
		UpdatedAt:    timeNow,
	}

	err := qs.WithContext(ctx).Create(user)
	if err != nil {
		return 0, repoerr.Wrap(err)
	}

	return user.ID, nil
}

func (u *User) GetUserInfoById(ctx context.Context, id int64) (*do.UserDo, error) {
	cacheKey := consts.UserCacheKeyByID(id)

	return u.getByKey(ctx, cacheKey, func() (*do.UserDo, error) {
		qs := u.q.User

		uinfo, err := qs.WithContext(ctx).Where(qs.ID.Eq(id)).First()
		if err != nil {
			return nil, repoerr.Wrap(err)
		}

		return &do.UserDo{
			ID:           uinfo.ID,
			Email:        uinfo.Email,
			Status:       uinfo.Status,
			StorageQuota: uinfo.StorageQuota,
			StorageUsed:  uinfo.StorageUsed,
			CreatedAt:    uinfo.CreatedAt,
			UpdatedAt:    uinfo.UpdatedAt,
		}, nil
	})
}

func (u *User) UpdateStorageUsed(ctx context.Context, id int64, storage int64) error {

	qs := u.q.User
	_, err := qs.WithContext(ctx).Where(qs.ID.Eq(id)).UpdateColumnSimple(qs.StorageUsed.Add(storage))
	if err != nil {
		fmt.Printf("gorm update error: %v\n", err)
		return repoerr.Wrap(err)
	}

	u.invalidateUserCache(ctx, id)

	return nil
}

func (u *User) getCachedRedis(ctx context.Context, key string) *do.UserDo {
	val, err := u.rds.Get(ctx, key).Result()
	if err != nil {
		return nil
	}

	var user do.UserDo
	if err := json.Unmarshal([]byte(val), &user); err != nil {
		return nil
	}

	return &user
}

func (u *User) setCachedRedis(ctx context.Context, key string, user *do.UserDo) error {
	data, err := json.Marshal(user)
	if err != nil {
		return repoerr.Wrap(err)
	}

	return repoerr.Wrap(u.rds.Set(ctx, key, data, time.Duration(consts.CacheTTLUser)*time.Second).Err())
}

func (u *User) setAllCaches(ctx context.Context, keys []string, user *do.UserDo) {
	for _, key := range keys {
		u.cacheManager.Set(key, user, 0)

		if err := u.setCachedRedis(ctx, key, user); err != nil {
			logger.Warn("failed to set user redis cache",
				zap.Error(err),
				zap.String("key", key),
				zap.String("user", gconv.String(user)),
			)
		}
	}
}

func (u *User) invalidateUserCache(ctx context.Context, id int64) {
	cacheKey := consts.UserCacheKeyByID(id)

	u.rds.Del(ctx, cacheKey)
	u.cacheManager.Remove(cacheKey)

	if err := u.cacheManager.Publish(ctx, cacheKey); err != nil {
		logger.Warn("failed to publish user cache invalidation",
			zap.Error(err),
			zap.String("key", cacheKey),
		)
	}
}

func (u *User) getByKey(
	ctx context.Context,
	cacheKey string,
	queryFn func() (*do.UserDo, error),
) (*do.UserDo, error) {
	if entry, ok := u.cacheManager.Get(cacheKey); ok {
		return entry.Data.(*do.UserDo), nil
	}

	if cached := u.getCachedRedis(ctx, cacheKey); cached != nil {
		u.cacheManager.Set(cacheKey, cached, 0)
		return cached, nil
	}

	result, err, _ := u.g.Do(cacheKey, func() (interface{}, error) {
		if cached := u.getCachedRedis(ctx, cacheKey); cached != nil {
			return cached, nil
		}

		user, err := queryFn()
		if err != nil {
			return nil, err
		}

		u.setAllCaches(ctx, []string{cacheKey}, user)
		return user, nil
	})
	if err != nil {
		return nil, err
	}

	user := result.(*do.UserDo)
	u.cacheManager.Set(cacheKey, user, 0)

	return user, nil
}
