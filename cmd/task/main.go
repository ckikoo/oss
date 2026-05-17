package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"oss/adaptor"
	"oss/config"
	"oss/internal/bootstrap"
	"oss/timer"
	"oss/utils/logger"
	"strings"
	"syscall"

	"go.uber.org/zap"
)

var mode = flag.String("mode", string(timer.ModeAll), fmt.Sprintf("timer mode: %s", strings.Join(timer.ValidModes(), ", ")))

func main() {
	config := config.InitConfig()
	logger.SetLogLevel(config.Server.LogLevel)

	db, err := bootstrap.InitMySQL(&config.Mysql)
	bootstrap.HandleFatal(err)
	defer db.Close()

	redisClient, err := bootstrap.InitRedis(&config.Redis)
	bootstrap.HandleFatal(err)
	defer redisClient.Close()

	log := logger.GetLogger()
	defer func() {
		_ = log.Sync()
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	newAdaptor := adaptor.NewAdaptor(config, db, redisClient, log)
	if newAdaptor == nil {
		log.Error("failed to initialize adaptor")
		os.Exit(1)
	}

	if err := newAdaptor.GetSub().Start(ctx); err != nil {
		log.Error("failed to start subscription", zap.Error(err))
		os.Exit(1)
	}
	defer newAdaptor.GetSub().Stop()

	selectedMode := timer.Mode(*mode)
	log.Info("starting timer process", zap.String("mode", string(selectedMode)))
	if err := timer.StartTimerMode(ctx, newAdaptor, selectedMode); err != nil {
		log.Error("timer process exited with error", zap.Error(err))
		os.Exit(1)
	}

	log.Info("timer process stopped", zap.String("mode", string(selectedMode)))
}
