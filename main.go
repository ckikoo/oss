package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"oss/adaptor"
	"oss/config"
	"oss/router"
	"oss/timer"
	"oss/utils/logger"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/go-redis/redis/v8"
	_ "github.com/go-sql-driver/mysql"
	"go.uber.org/zap"
)

func main() {

	config := config.InitConfig()
	logger.SetLogLevel(config.Server.LogLevel)

	db, err := initMysql(&config.Mysql)
	handleErr(err)
	defer db.Close()

	logger.Debug("mysql connect success")
	redisClient, err := initRedis(&config.Redis)
	handleErr(err)
	defer redisClient.Close()

	logger.Debug("redis connect success")

	startServer(context.Background(), config, db, redisClient, logger.GetLogger())

	defer func() {
		if config.Server.Env == "dev" {
			redisClient.FlushDBAsync(context.Background())
		}

		logger.GetLogger().Sync()

	}()
}

func startServer(ctx context.Context, conf *config.Config, db *sql.DB, redis *redis.Client, logger *zap.Logger) {
	newAdaptor := adaptor.NewAdaptor(conf, db, redis, logger)

	if newAdaptor == nil {
		logger.Error("failed to initialize adaptor")
		return
	}

	if err := newAdaptor.GetSub().Start(ctx); err != nil {
		log.Fatal(err)
	}

	defer newAdaptor.GetSub().Stop()

	address := fmt.Sprintf("%s:%d", conf.Server.Host, conf.Server.Port)
	h := server.Default(server.WithHostPorts(address))

	deps := router.NewRouterDeps(newAdaptor)
	router.RegisterRoutes(h, deps, newAdaptor)

	// // 启动后台定时任务，并在异常退出时重启
	go func() {
		for {
			func() {
				defer func() {
					if r := recover(); r != nil {
						logger.Error("timer goroutine panic", zap.Any("panic", r), zap.Stack("stack"))
					}
				}()
				timer.StartTimer(ctx, newAdaptor)
			}()

			// timer.StartTimer 只会在 ctx 取消或发生不可恢复错误时返回，短暂停顿后重启
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}()

	h.Spin()
}

func initMysql(conf *config.Mysql) (*sql.DB, error) {
	conf.MaxIdle = max(conf.MaxIdle, 5)
	conf.MaxOpen = max(conf.MaxOpen, 10)

	if conf.MaxOpen < conf.MaxIdle {
		conf.MaxOpen = conf.MaxIdle
	}

	dsn := conf.GetDsn()
	sqlDB, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}

	if err = sqlDB.Ping(); err != nil {
		return nil, err
	}

	rows, err := sqlDB.Query("show tables")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, fmt.Errorf("database is empty, please run the SQL initialization script")
	}

	sqlDB.SetMaxIdleConns(conf.MaxIdle)
	sqlDB.SetMaxOpenConns(conf.MaxOpen)
	return sqlDB, nil
}

func initRedis(conf *config.Redis) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         conf.Addr,
		Password:     conf.Password,
		DB:           conf.DB,
		MinIdleConns: conf.MaxIdle,
		PoolSize:     conf.MaxOpen,
	})

	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, err
	}

	return client, nil
}
func handleErr(err error) {
	if err != nil {
		logger.GetLogger().Error("fatal error", zap.Error(err))
		os.Exit(1)
	}
}

// func initMongoDB(conf *config.Mongo) error {

// }
