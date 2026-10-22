package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sywesk/ocea-exporter/pkg/homeassistant"
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
	Debug bool `yaml:"debug"`
}

func (c *config) setFromEnv() error {
	stringsByKey := map[string]*string{
		"USERNAME": &c.Username, "PASSWORD": &c.Password, "POLL_INTERVAL": &c.PollInterval,
		"STATE_FILE_PATH": &c.StateFilePath, "PROMETHEUS_LISTEN_ADDR": &c.Prometheus.ListenAddr,
		"HOME_ASSISTANT_BROKER_ADDR": &c.HomeAssistant.BrokerAddr,
		"HOME_ASSISTANT_USERNAME":    &c.HomeAssistant.Username, "HOME_ASSISTANT_PASSWORD": &c.HomeAssistant.Password,
	}
	for key, target := range stringsByKey {
		if value := os.Getenv(EnvironmentVariablePrefix + key); value != "" {
			*target = value
		}
	}
	for key, target := range map[string]*bool{"PROMETHEUS_ENABLED": &c.Prometheus.Enabled, "HOME_ASSISTANT_ENABLED": &c.HomeAssistant.Enabled, "DEBUG": &c.Debug} {
		name := EnvironmentVariablePrefix + key
		value := os.Getenv(name)
		if value == "" {
			continue
		}
		parsed, err := strconv.ParseBool(strings.ToLower(value))
		if err != nil {
			return fmt.Errorf("invalid boolean for %s: %w", name, err)
		}
		*target = parsed
	}
	return nil
}

func (c *config) setDefaults() error {
	if c.PollInterval == "" {
		c.PollInterval = "30m"
	}
	if c.StateFilePath == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return fmt.Errorf("getting user config directory: %w", err)
		}
		c.StateFilePath = filepath.Join(dir, "ocea-exporter", "state.json")
	}
	if c.Prometheus.ListenAddr == "" {
		c.Prometheus.ListenAddr = "127.0.0.1:9001"
	}
	return nil
}

func (c config) validate() error {
	if c.Username == "" {
		return fmt.Errorf("username must be set")
	}
	if c.Password == "" {
		return fmt.Errorf("password must be set")
	}
	interval, err := time.ParseDuration(c.PollInterval)
	if err != nil {
		return fmt.Errorf("invalid poll_interval: %w", err)
	}
	if interval <= 0 {
		return fmt.Errorf("poll_interval must be positive")
	}
	if c.HomeAssistant.Enabled {
		if err := (homeassistant.MQTTParams{Host: c.HomeAssistant.BrokerAddr}).Validate(); err != nil {
			return fmt.Errorf("invalid home_assistant.broker_addr: %w", err)
		}
	}
	return nil
}

var globalConfig config

func loadConfig(paths ...string) error {
	if len(paths) > 1 {
		return fmt.Errorf("expected at most one configuration file")
	}
	var next config
	if len(paths) == 1 {
		contents, err := os.ReadFile(paths[0])
		if err != nil {
			return fmt.Errorf("reading configuration: %w", err)
		}
		decoder := yaml.NewDecoder(bytes.NewReader(contents))
		decoder.KnownFields(true)
		if err := decoder.Decode(&next); err != nil && err != io.EOF {
			return fmt.Errorf("decoding configuration: %w", err)
		}
		var extra interface{}
		if err := decoder.Decode(&extra); err != io.EOF {
			return fmt.Errorf("configuration must contain a single YAML document")
		}
	}
	if err := next.setFromEnv(); err != nil {
		return err
	}
	if err := next.setDefaults(); err != nil {
		return err
	}
	if err := next.validate(); err != nil {
		return err
	}
	globalConfig = next
	return nil
}

func getConfig() config { return globalConfig }
