package main

import (
	"github.com/sywesk/ocea-exporter/pkg/counterfetcher"
	"go.uber.org/zap"
	"os"
	"time"
)

func main() {
	logger, err := zap.NewDevelopment()
	if err != nil {
		panic("failed to init zap: " + err.Error())
	}

	zap.ReplaceGlobals(logger)

	if len(os.Args) != 2 {
		println("usage: ocea-exporter <config file>")
		os.Exit(1)
	}

	if err := loadConfig(os.Args[1]); err != nil {
		zap.L().Fatal("failed to load configuration", zap.Error(err))
	}

	fetcher, err := counterfetcher.New(counterfetcher.Settings{
		Username: getConfig().Username,
		Password: getConfig().Password,
	})
	if err != nil {
		zap.L().Fatal("failed to create a counter fetcher", zap.Error(err))
	}

	err = fetcher.Start()
	if err != nil {
		zap.L().Fatal("failed to start counter fetcher", zap.Error(err))
	}

	setupMetricsHandler()

	for {
		time.Sleep(1 * time.Minute)
	}
}
