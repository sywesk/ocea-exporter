package main

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
)

type config struct {
	Username    string `yaml:"username"`
	Password    string `yaml:"password"`
	MetricsAddr string `yaml:"metrics_addr"`
}

var globalConfig config

func LoadConfig(path string) error {
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

func GetConfig() config {
	return globalConfig
}