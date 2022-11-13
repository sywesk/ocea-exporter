package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"net/http"
)

var (
	monthToDate = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ocea",
		Subsystem: "metering",
		Name:      "month_to_date",
	}, []string{"fluid"})

	yearToDate = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ocea",
		Subsystem: "metering",
		Name:      "year_to_date",
	}, []string{"fluid"})
)

func setupMetricsHandler() {
	listenAddr := GetConfig().MetricsAddr
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
