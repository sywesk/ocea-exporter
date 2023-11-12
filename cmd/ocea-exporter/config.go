package main

import (
	"fmt"
	"os"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

const EnvironmentVariablePrefix = "OCEA_EXPORTER_"

type config struct {
	Username      string `yaml:"username"`
	Password      string `yaml:"password"`
	PollInterval  string `yaml:"poll_interval"`
	StateFilePath string `yaml:"state_file_path"`
	Prometheus    struct {
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

func (c *config) setFromEnv() {
	setStringFromEnv(&c.Username, EnvironmentVariablePrefix+"USERNAME")
	setStringFromEnv(&c.Password, EnvironmentVariablePrefix+"PASSWORD")
	setStringFromEnv(&c.StateFilePath, EnvironmentVariablePrefix+"STATE_FILE_PATH")
	setStringFromEnv(&c.PollInterval, EnvironmentVariablePrefix+"POLL_INTERVAL")
	setBoolFromEnv(&c.Prometheus.Enabled, EnvironmentVariablePrefix+"PROMETHEUS_ENABLED")
	setStringFromEnv(&c.Prometheus.ListenAddr, EnvironmentVariablePrefix+"PROMETHEUS_LISTEN_ADDR")
	setBoolFromEnv(&c.HomeAssistant.Enabled, EnvironmentVariablePrefix+"HOME_ASSISTANT_ENABLED")
	setStringFromEnv(&c.HomeAssistant.BrokerAddr, EnvironmentVariablePrefix+"HOME_ASSISTANT_BROKER_ADDR")
	setStringFromEnv(&c.HomeAssistant.Username, EnvironmentVariablePrefix+"HOME_ASSISTANT_USERNAME")
	setStringFromEnv(&c.HomeAssistant.Password, EnvironmentVariablePrefix+"HOME_ASSISTANT_PASSWORD")
}

func (c *config) setDefaults() {
	if c.PollInterval == "" {
		c.PollInterval = "30m"
	}

	if c.StateFilePath == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			panic(fmt.Sprintf("getting user config dir: %v", err))
		}
		c.StateFilePath = path.Join(dir, "ocea-exporter", "state.json")
	}

	if c.Prometheus.ListenAddr == "" {
		c.Prometheus.ListenAddr = "127.0.0.1:9001"
	}
}

func (c *config) validate() error {
	if c.Username == "" {
		return fmt.Errorf("username must be set")
	}
	if c.Password == "" {
		return fmt.Errorf("password must be set")
	}
	return nil
}

var globalConfig config

func loadConfig(path ...string) error {
	// Load the configuration from a file if specified.
	if len(path) != 0 {
		contents, err := os.ReadFile(path[0])
		if err != nil {
			return fmt.Errorf("failed to read file: %w", err)
		}

		err = yaml.Unmarshal(contents, &globalConfig)
		if err != nil {
			return fmt.Errorf("failed to unmarshal config file: %w", err)
		}
	}

	// The override with env vars if specified.
	globalConfig.setFromEnv()
	globalConfig.setDefaults()

	return globalConfig.validate()
}

func getConfig() config {
	return globalConfig
}

func setStringFromEnv(str *string, envVarName string) {
	value := os.Getenv(envVarName)
	if value == "" {
		return
	}
	*str = value
}

func setBoolFromEnv(b *bool, envVarName string) {
	value := os.Getenv(envVarName)
	if value == "" {
		return
	}

	valueLower := strings.ToLower(value)

	if valueLower == "t" || value == "true" || value == "1" {
		*b = true
		return
	}
	if valueLower == "f" || value == "false" || value == "0" {
		*b = false
		return
	}

	panic(fmt.Sprintf("invalid boolean value '%s' for env var '%s'", value, envVarName))
}
