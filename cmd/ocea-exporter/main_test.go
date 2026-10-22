package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRunStopsWithCancelledContext(t *testing.T) {
	cfg := config{Username: "test", Password: "test", PollInterval: "3h", StateFilePath: filepath.Join(t.TempDir(), "state.json")}
	cfg.Prometheus.Enabled = true
	cfg.Prometheus.ListenAddr = "127.0.0.1:0"
	cfg.HomeAssistant.Enabled = true
	cfg.HomeAssistant.BrokerAddr = "127.0.0.1:1883"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// A pre-cancelled lifetime starts no authentication or broker traffic.
	if err := run(ctx, cfg); err != nil {
		t.Fatal(err)
	}
}

func TestRunReportsCorruptStateWithoutStartingWorkers(t *testing.T) {
	file := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(file, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := config{Username: "test", Password: "test", PollInterval: "3h", StateFilePath: file}
	cfg.Prometheus.Enabled = true
	cfg.Prometheus.ListenAddr = "127.0.0.1:0"
	if err := run(context.Background(), cfg); err == nil {
		t.Fatal("startup hid corrupt state")
	}
}
