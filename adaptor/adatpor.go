package adaptor

import (
	"database/sql"
	"oss/adaptor/storage"
	"oss/adaptor/storage/local"
	"oss/config"

	"github.com/go-redis/redis/v8"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type IAdaptor interface {
	GetConfig() *config.Config
	GetDB() *sql.DB
	GetRedis() *redis.Client
	GetStorage() storage.IStorage
	GetGORM() *gorm.DB
}
type Adaptor struct {
	conf    *config.Config
	db      *sql.DB
	redis   *redis.Client
	storage storage.IStorage
	gormDB  *gorm.DB
}

func NewAdaptor(conf *config.Config, db *sql.DB, redis *redis.Client) *Adaptor {
	gormDB, err := gorm.Open(mysql.New(mysql.Config{Conn: db}), &gorm.Config{})
	if err != nil {
		panic(err)
	}

	return &Adaptor{
		conf:    conf,
		db:      db,
		redis:   redis,
		storage: local.New(conf.Server.SaveDir),
		gormDB:  gormDB,
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

func (a *Adaptor) GetGORM() *gorm.DB {
	return a.gormDB
}
