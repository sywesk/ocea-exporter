package main

import (
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"net/http"
)

func setupPrometheusMetricsHandler() {
	if !getConfig().Prometheus.Enabled {
		zap.L().Info("prometheus exporter is disabled")
		return
	}

	go listenProm()
}

func listenProm() {
	listenAddr := getConfig().Prometheus.ListenAddr
	if listenAddr == "" {
		listenAddr = "127.0.0.1:9001"
	}

	zap.L().Info("serving metrics", zap.String("url", listenAddr+"/metrics"))

	http.Handle("/metrics", promhttp.Handler())

	err := http.ListenAndServe(listenAddr, nil)
	if err != nil {
		zap.L().Error("failed to listen and serve metrics", zap.Error(err))
	}
}
