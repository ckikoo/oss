package adaptor

import (
	"database/sql"

	"oss/adaptor/storage"
	"oss/adaptor/storage/local"
	"oss/adaptor/tx"
	"oss/config"
	"oss/utils/cache"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type IAdaptor interface {
	GetConfig() *config.Config
	GetDB() *sql.DB
	GetRedis() *redis.Client
	GetStorage() storage.IStorage
	GetGORM() *gorm.DB
	GetTxManager() tx.ITxManager

	GetCache() cache.IManager  // repo 依赖
	GetSub() cache.ISubscriber // main 启动用于监听事件
	GetLogger() *zap.Logger
}

type Adaptor struct {
	conf      *config.Config
	db        *sql.DB
	redis     *redis.Client
	storage   storage.IStorage
	gormDB    *gorm.DB
	txManager tx.ITxManager
	cm        *cache.Manager
	logger    *zap.Logger
}

func NewAdaptor(conf *config.Config, db *sql.DB, redis *redis.Client, logger *zap.Logger) *Adaptor {
	// gormLogger := gormlogger.New(
	// 	log.New(os.Stdout, "", log.LstdFlags),
	// 	gormlogger.Config{
	// 		SlowThreshold:             time.Second,
	// 		LogLevel:                  gormlogger.Info,
	// 		IgnoreRecordNotFoundError: true,
	// 		Colorful:                  true,
	// 	},
	// )

	gormDB, err := gorm.Open(mysql.New(mysql.Config{Conn: db}), &gorm.Config{})
	if err != nil {
		logger.Error("failed to connect to database with gorm", zap.Error(err))
		return nil
	}

	return &Adaptor{
		conf:      conf,
		db:        db,
		redis:     redis,
		storage:   local.New(conf.Server.SaveDir),
		gormDB:    gormDB,
		txManager: tx.NewGormTxManager(gormDB),
		cm:        cache.NewManager(redis, logger),
		logger:    logger,
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

func (a *Adaptor) GetTxManager() tx.ITxManager {
	return a.txManager
}
func (a *Adaptor) GetCache() cache.IManager  { return a.cm }
func (a *Adaptor) GetSub() cache.ISubscriber { return a.cm }
func (a *Adaptor) GetLogger() *zap.Logger {
	return a.logger
}
