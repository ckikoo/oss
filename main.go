package main

import (
	"database/sql"
	"fmt"
	"oss/adaptor"
	"oss/config"
	"oss/router"
	"oss/utils/logger"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/go-redis/redis"
	_ "github.com/go-sql-driver/mysql"
)

func main() {

	config := config.InitConfig()
	logger.SetLogLevel(config.Server.LogLevel)

	db, err := initMysql(&config.Mysql)
	handleErr(err)
	logger.Debug("mysql connect success")
	redisClient, err := initRedis(&config.Redis)
	handleErr(err)
	logger.Debug("redis connect success")

	startServer(config, db, redisClient)

}

func startServer(conf *config.Config, db *sql.DB, redis *redis.Client) {
	newAdaptor := adaptor.NewAdaptor(conf, db, redis)

	address := fmt.Sprintf("%s:%d", conf.Server.Host, conf.Server.Port)
	h := server.Default(server.WithHostPorts(address))
	router.RegisterRoutes(h, newAdaptor)

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

	if err := client.Ping().Err(); err != nil {
		return nil, err
	}

	return client, nil
}
func handleErr(err error) {
	if err != nil {
		panic(err)
	}
}

// func initMongoDB(conf *config.Mongo) error {

// }
