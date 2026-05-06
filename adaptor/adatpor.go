package adaptor

import (
	"database/sql"
	"oss/adaptor/storage"
	"oss/adaptor/storage/local"
	"oss/config"

	"github.com/go-redis/redis"
)

type IAdaptor interface {
	GetConfig() *config.Config
	GetDB() *sql.DB
	GetRedis() *redis.Client
	GetStorage() storage.IStorage // 新增
}
type Adaptor struct {
	conf    *config.Config
	db      *sql.DB
	redis   *redis.Client
	storage storage.IStorage
}

func NewAdaptor(conf *config.Config, db *sql.DB, redis *redis.Client) *Adaptor {
	return &Adaptor{
		conf:    conf,
		db:      db,
		redis:   redis,
		storage: local.New(conf.Server.SaveDir),
	}
}

func (a *Adaptor) GetConfig() *config.Config {
	return a.conf
}

func (a *Adaptor) GetDB() *sql.DB {
	return a.db
}

func (a *Adaptor) GetRedis() *redis.Client {
	return a.redis
}

func (a *Adaptor) GetStorage() storage.IStorage {
	return a.storage
}
