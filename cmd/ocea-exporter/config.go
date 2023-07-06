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
		Enabled    bool   `yaml:"enabled"`
		ListenAddr string `yaml:"listen_addr"`
	} `yaml:"prometheus"`
	HomeAssistant struct {
		Enabled    bool   `yaml:"enabled"`
		BrokerAddr string `yaml:"broker_addr"`
		Username   string `yaml:"username"`
		Password   string `yaml:"password"`
	} `yaml:"home_assistant"`
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
