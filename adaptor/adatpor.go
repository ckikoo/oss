package adaptor

import (
	"database/sql"
	"log"
	"os"
	"strings"
	"time"

	"oss/adaptor/storage"
	"oss/adaptor/storage/local"
	storage_s3 "oss/adaptor/storage/s3"
	"oss/adaptor/tx"
	"oss/config"
	"oss/utils/cache"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
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

func NewAdaptor(conf *config.Config, db *sql.DB, redis *redis.Client, log1 *zap.Logger) *Adaptor {
	gormLogger := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold:             500 * time.Millisecond,
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
			ParameterizedQueries:      true,
			Colorful:                  false,
		},
	)

	gormDB, err := gorm.Open(mysql.New(mysql.Config{Conn: db}), &gorm.Config{Logger: gormLogger})
	if err != nil {
		log1.Error("failed to connect to database with gorm", zap.Error(err))
		return nil
	}

	var storageBackend storage.IStorage
	storageType := strings.ToLower(strings.TrimSpace(conf.Storage.Type))
	switch storageType {
	case "s3", "oss", "cos":
		providerCfg := conf.Storage.GetProviderConfig(storageType)
		s3Storage, err := storage_s3.New(providerCfg)
		if err != nil {
			log1.Error("failed to initialize object storage", zap.Error(err))
			return nil
		}
		storageBackend = s3Storage
	case "", "local":
		baseDir := conf.Storage.Local.BaseDir
		if baseDir == "" {
			baseDir = conf.Server.SaveDir
		}
		storageBackend = local.New(baseDir)
	default:
		log1.Error("unsupported storage type", zap.String("storage_type", conf.Storage.Type))
		return nil
	}

	return &Adaptor{
		conf:      conf,
		db:        db,
		redis:     redis,
		storage:   storageBackend,
		gormDB:    gormDB,
		txManager: tx.NewGormTxManager(gormDB),
		cm:        cache.NewManager(redis, log1),
		logger:    log1,
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
