package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"oss/adaptor"
	"oss/config"
	"oss/consts"
	"oss/internal/bootstrap"
	"oss/router"
	"oss/utils/logger"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
)

func main() {
	config := config.InitConfig()
	logger.SetLogLevel(config.Server.LogLevel)
	db, err := bootstrap.InitMySQL(&config.Mysql)
	bootstrap.HandleFatal(err)
	defer db.Close()

	logger.Debug("mysql connect success")
	redisClient, err := bootstrap.InitRedis(&config.Redis)
	bootstrap.HandleFatal(err)
	defer redisClient.Close()

	logger.Debug("redis connect success")

	defer func() {
		if config.Server.Env == "dev" {
			redisClient.FlushDBAsync(context.Background())
		}

		_ = logger.GetLogger().Sync()
	}()

	startServer(context.Background(), config, db, redisClient, logger.GetLogger())
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
	h := server.Default(
		server.WithHostPorts(address),
		server.WithMaxRequestBodySize(consts.MaxPartSize))

	deps := router.NewRouterDeps(newAdaptor)
	router.RegisterRoutes(h, deps, newAdaptor)

	h.Spin()
}
