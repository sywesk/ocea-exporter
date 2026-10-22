package main

import (
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestMetricsServerServesPrivateRegistryAndStops(t *testing.T) {
	var cfg config
	cfg.Prometheus.Enabled = true
	cfg.Prometheus.ListenAddr = "127.0.0.1:0"
	registry := prometheus.NewRegistry()
	gauge := prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_metric", Help: "Test metric."})
	gauge.Set(42)
	registry.MustRegister(gauge)
	server, _, err := startPrometheusServer(cfg, registry)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	resp, err := http.Get("http://" + server.Addr + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != 200 || !strings.Contains(string(body), "test_metric 42") {
		t.Fatalf("unexpected metrics: %s, %v", body, err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := net.Dial("tcp", server.Addr); err == nil {
		t.Fatal("listener did not close")
	}
}

func TestMetricsBindFailureIsStartupError(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var cfg config
	cfg.Prometheus.Enabled = true
	cfg.Prometheus.ListenAddr = listener.Addr().String()
	if _, _, err := startPrometheusServer(cfg, prometheus.NewRegistry()); err == nil {
		t.Fatal("bind failure hidden")
	}
}
