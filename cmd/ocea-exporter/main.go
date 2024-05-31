package main

import (
	"os"
	"time"

	"github.com/sywesk/ocea-exporter/pkg/counterfetcher"
	"github.com/sywesk/ocea-exporter/pkg/homeassistant"
	"go.uber.org/zap"
)

func main() {
	zapCfg := zap.NewDevelopmentConfig()
	logger, err := zapCfg.Build()
	if err != nil {
		panic("failed to init zap: " + err.Error())
	}
	zap.ReplaceGlobals(logger)

	if len(os.Args) >= 3 {
		println("usage: ocea-exporter [config_file]")
		println("  config_file: optional path to a configuration file")
		os.Exit(1)
	}

	if err := loadConfig(os.Args[1:]...); err != nil {
		zap.L().Fatal("failed to load configuration", zap.Error(err))
	}
	if !getConfig().Debug {
		zapCfg.Level.SetLevel(zap.InfoLevel)
	}

	fetcher, err := counterfetcher.New(buildFetcherSettings())
	if err != nil {
		zap.L().Fatal("failed to create a counter fetcher", zap.Error(err))
	}

	err = fetcher.Start()
	if err != nil {
		zap.L().Fatal("failed to start counter fetcher", zap.Error(err))
	}

	setupPrometheusMetricsHandler()
	startHomeAssistantIntegration(fetcher)

	for {
		time.Sleep(1 * time.Minute)
	}
}

func startHomeAssistantIntegration(fetcher *counterfetcher.CounterFetcher) {
	cfg := getConfig()

	if !cfg.HomeAssistant.Enabled {
		zap.L().Info("homeassistant integration is disabled")
		return
	}

	ha, receiver := homeassistant.New(homeassistant.MQTTParams{
		Host:     cfg.HomeAssistant.BrokerAddr,
		Username: cfg.HomeAssistant.Username,
		Password: cfg.HomeAssistant.Password,
	})
	ha.Start()

	fetcher.RegisterListener(receiver)
}

func buildFetcherSettings() counterfetcher.Settings {
	cfg := getConfig()

	intervalDuration, err := time.ParseDuration(cfg.PollInterval)
	if err != nil {
		zap.L().Fatal("invalid poll_interval", zap.String("input", cfg.PollInterval), zap.Error(err))
	}

	return counterfetcher.Settings{
		StateFilePath: cfg.StateFilePath,
		Username:      cfg.Username,
		Password:      cfg.Password,
		PollInterval:  intervalDuration,
	}
}
