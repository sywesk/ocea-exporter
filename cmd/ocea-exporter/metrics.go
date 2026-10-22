package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Bind synchronously: an invalid address is a startup error, not a silently
// terminated background goroutine. Each server owns its own HTTP mux.
func startPrometheusServer(cfg config, registry prometheus.Gatherer) (*http.Server, <-chan error, error) {
	if !cfg.Prometheus.Enabled {
		return nil, nil, nil
	}
	listener, err := net.Listen("tcp", cfg.Prometheus.ListenAddr)
	if err != nil {
		return nil, nil, fmt.Errorf("listening for metrics: %w", err)
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	server := &http.Server{Addr: listener.Addr().String(), Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: time.Minute}
	failures := make(chan error, 1)
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			failures <- err
		}
	}()
	return server, failures, nil
}
