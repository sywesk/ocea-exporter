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
	zap.L().Info("serving metrics", zap.String("url", listenAddr+"/metrics"))
	http.Handle("/metrics", promhttp.Handler())
	if err := http.ListenAndServe(listenAddr, nil); err != nil {
		zap.L().Error("failed to listen and serve metrics", zap.Error(err))
	}
}
