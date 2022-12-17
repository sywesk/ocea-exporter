package main

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
)

type config struct {
	Username     string `yaml:"username"`
	Password     string `yaml:"password"`
	PollInterval string `yaml:"poll_interval"`
	Prometheus   struct {
		Enabled    bool   `json:"enabled"`
		ListenAddr string `yaml:"listen_addr"`
	} `json:"prometheus"`
}

var globalConfig config

func loadConfig(path string) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	err = yaml.Unmarshal(contents, &globalConfig)
	if err != nil {
		return fmt.Errorf("failed to unmarshal config file: %w", err)
	}

	return nil
}

func getConfig() config {
	return globalConfig
}
