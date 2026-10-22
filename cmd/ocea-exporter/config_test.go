package main

import (
	"os"
	"path/filepath"
	"testing"
)

func configFile(t *testing.T, extra string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(file, []byte("username: user\npassword: secret\n"+extra), 0600); err != nil {
		t.Fatal(err)
	}
	return file
}

func resetConfig(t *testing.T) {
	t.Helper()
	previous := globalConfig
	globalConfig = config{}
	t.Cleanup(func() { globalConfig = previous })
	for _, key := range []string{"USERNAME", "PASSWORD", "POLL_INTERVAL", "STATE_FILE_PATH", "PROMETHEUS_ENABLED", "PROMETHEUS_LISTEN_ADDR", "HOME_ASSISTANT_ENABLED", "HOME_ASSISTANT_BROKER_ADDR", "HOME_ASSISTANT_USERNAME", "HOME_ASSISTANT_PASSWORD", "DEBUG"} {
		t.Setenv(EnvironmentVariablePrefix+key, "")
	}
}

func TestConfigFileEnvironmentAndDefaults(t *testing.T) {
	resetConfig(t)
	t.Setenv(EnvironmentVariablePrefix+"USERNAME", "override")
	t.Setenv(EnvironmentVariablePrefix+"HOME_ASSISTANT_ENABLED", "false")
	if err := loadConfig(configFile(t, "prometheus:\n  enabled: true\nhome_assistant:\n  enabled: true\n")); err != nil {
		t.Fatal(err)
	}
	c := getConfig()
	if c.Username != "override" || c.Password != "secret" || c.PollInterval != "30m" || c.StateFilePath == "" || !c.Prometheus.Enabled || c.HomeAssistant.Enabled {
		t.Fatalf("unexpected config: %+v", c)
	}
}

func TestConfigRejectsInvalidIntervals(t *testing.T) {
	for _, interval := range []string{"0s", "-1m", "1d", "garbage"} {
		t.Run(interval, func(t *testing.T) {
			resetConfig(t)
			if err := loadConfig(configFile(t, "poll_interval: "+interval+"\n")); err == nil {
				t.Fatal("accepted invalid interval")
			}
		})
	}
}

func TestConfigBooleanEnvironment(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{{"TRUE", true}, {"False", false}, {"1", true}, {"0", false}, {"t", true}, {"f", false}} {
		t.Run(tc.value, func(t *testing.T) {
			resetConfig(t)
			t.Setenv(EnvironmentVariablePrefix+"DEBUG", tc.value)
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("boolean caused panic: %v", p)
				}
			}()
			if err := loadConfig(configFile(t, "")); err != nil {
				t.Fatal(err)
			}
			if getConfig().Debug != tc.want {
				t.Fatal("wrong boolean value")
			}
		})
	}
}

func TestInvalidBooleanReturnsError(t *testing.T) {
	resetConfig(t)
	t.Setenv(EnvironmentVariablePrefix+"DEBUG", "maybe")
	defer func() {
		if p := recover(); p != nil {
			t.Errorf("configuration panicked: %v", p)
		}
	}()
	if err := loadConfig(configFile(t, "")); err == nil {
		t.Fatal("accepted invalid boolean")
	}
}

func TestConfigRejectsUnknownKeys(t *testing.T) {
	resetConfig(t)
	if err := loadConfig(configFile(t, "poll_intervall: 1s\n")); err == nil {
		t.Fatal("accepted misspelled setting")
	}
}

func TestConfigReloadDoesNotKeepPreviousValues(t *testing.T) {
	resetConfig(t)
	if err := loadConfig(configFile(t, "debug: true\n")); err != nil {
		t.Fatal(err)
	}
	if err := loadConfig(configFile(t, "")); err != nil {
		t.Fatal(err)
	}
	if getConfig().Debug {
		t.Fatal("previous configuration leaked into next load")
	}
}

func TestConfigRejectsInvalidBrokerOnlyWhenEnabled(t *testing.T) {
	for _, enabled := range []string{"true", "false"} {
		t.Run(enabled, func(t *testing.T) {
			resetConfig(t)
			err := loadConfig(configFile(t, "home_assistant:\n  enabled: "+enabled+"\n  broker_addr: http://broker\n"))
			if (err != nil) != (enabled == "true") {
				t.Fatalf("unexpected broker validation: %v", err)
			}
		})
	}
}
