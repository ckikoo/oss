package bootstrap

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"oss/config"
	"oss/utils/logger"

	"github.com/go-redis/redis/v8"
	_ "github.com/go-sql-driver/mysql"
	"go.uber.org/zap"
)

func InitMySQL(conf *config.Mysql) (*sql.DB, error) {
	conf.MaxIdle = max(conf.MaxIdle, 5)
	conf.MaxOpen = max(conf.MaxOpen, 10)

	if conf.MaxOpen < conf.MaxIdle {
		conf.MaxOpen = conf.MaxIdle
	}

	sqlDB, err := sql.Open("mysql", conf.GetDsn())
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

func InitRedis(conf *config.Redis) (*redis.Client, error) {
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

func HandleFatal(err error) {
	if err == nil {
		return
	}

	logger.GetLogger().Error("fatal error", zap.Error(err))
	os.Exit(1)
}
