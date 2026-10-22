package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/sywesk/ocea-exporter/pkg/counterfetcher"
	"github.com/sywesk/ocea-exporter/pkg/homeassistant"
	"go.uber.org/zap"
)

func main() {
	if len(os.Args) > 2 {
		fmt.Fprintln(os.Stderr, "usage: ocea-exporter [config_file]")
		os.Exit(1)
	}
	if err := loadConfig(os.Args[1:]...); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	cfg := getConfig()
	logConfig := zap.NewDevelopmentConfig()
	if !cfg.Debug {
		logConfig.Level.SetLevel(zap.InfoLevel)
	}
	logger, err := logConfig.Build(zap.AddStacktrace(zap.ErrorLevel))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	zap.ReplaceGlobals(logger)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err = run(ctx, cfg)
	stop()
	if err != nil {
		logger.Error("exporter stopped", zap.Error(err))
	}
	_ = logger.Sync()
	if err != nil {
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg config) error {
	interval, err := time.ParseDuration(cfg.PollInterval)
	if err != nil {
		return fmt.Errorf("invalid poll interval: %w", err)
	}
	fetcher, err := counterfetcher.New(counterfetcher.Settings{StateFilePath: cfg.StateFilePath, Username: cfg.Username, Password: cfg.Password, PollInterval: interval})
	if err != nil {
		return err
	}
	registry := prometheus.NewRegistry()
	registry.MustRegister(fetcher, collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	server, serverErrors, err := startPrometheusServer(cfg, registry)
	if err != nil {
		return err
	}
	if server != nil {
		defer server.Close()
	}
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var ha *homeassistant.MQTT
	if cfg.HomeAssistant.Enabled {
		integration, receiver := homeassistant.New(homeassistant.MQTTParams{Host: cfg.HomeAssistant.BrokerAddr, Username: cfg.HomeAssistant.Username, Password: cfg.HomeAssistant.Password})
		ha = &integration
		fetcher.RegisterListener(receiver)
	}
	if err := fetcher.Start(workerCtx); err != nil {
		return err
	}
	if ha != nil {
		go ha.Run(workerCtx)
	}
	select {
	case <-ctx.Done():
	case err = <-serverErrors:
	}
	cancel()
	<-fetcher.Done()
	if ha != nil {
		<-ha.Done()
	}
	if server != nil {
		shutdownCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if shutdownErr := server.Shutdown(shutdownCtx); shutdownErr != nil && err == nil {
			err = shutdownErr
		}
	}
	return err
}
