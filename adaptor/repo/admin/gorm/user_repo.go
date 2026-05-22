package gorm

import (
	"context"
	"encoding/json"
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
	"oss/adaptor/repo/repocache"
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
	cacheEnabled bool
}

var _ admin.IUser = (*User)(nil)

func NewUserRepo(a adaptor.IAdaptor) *User {
	return &User{
		db:           a.GetGORM(),
		q:            query.Use(a.GetGORM()),
		rds:          a.GetRedis(),
		g:            &singleflight.Group{},
		cacheManager: a.GetCache(),
		cacheEnabled: true,
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
		cacheEnabled: false,
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
		return repoerr.Wrap(err)
	}

	u.invalidateUserCache(ctx, id)

	return nil
}

func (u *User) getCachedRedis(ctx context.Context, key string) *do.UserDo {
	if u.rds == nil {
		return nil
	}
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
	if u.cacheManager == nil {
		return
	}
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

	repocache.Invalidator{
		RDS:          u.rds,
		Local:        u.cacheManager,
		DoubleDelete: true,
		LogName:      "user",
	}.AfterCommit(ctx, cacheKey)
}

func (u *User) getByKey(
	ctx context.Context,
	cacheKey string,
	queryFn func() (*do.UserDo, error),
) (*do.UserDo, error) {
	return repocache.Accessor[*do.UserDo]{
		RDS:     u.rds,
		Local:   u.cacheManager,
		Group:   u.g,
		TTL:     time.Duration(consts.CacheTTLUser) * time.Second,
		Enabled: u.cacheEnabled,
		LogName: "user",
	}.Get(ctx, cacheKey, func(context.Context) (*do.UserDo, error) {
		return queryFn()
	})
}
