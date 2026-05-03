package adaptor

import (
	"database/sql"
	"oss/config"

	"github.com/go-redis/redis"
)

type IAdaptor interface {
	GetConfig() *config.Config
	GetDB() *sql.DB
	GetRedis() *redis.Client
}
type Adaptor struct {
	conf  *config.Config
	db    *sql.DB
	redis *redis.Client
}

func NewAdaptor(conf *config.Config, db *sql.DB, redis *redis.Client) *Adaptor {
	return &Adaptor{
		conf:  conf,
		db:    db,
		redis: redis,
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
