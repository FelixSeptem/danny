package config

import (
	"strings"
	"testing"
)

func TestLoadFromEnvUsesStableLocalDefaults(t *testing.T) {
	cfg, err := LoadFromEnv(func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}
	if cfg.ListenAddress != "127.0.0.1:8080" {
		t.Fatalf("ListenAddress = %q, want 127.0.0.1:8080", cfg.ListenAddress)
	}
	if cfg.StorageDriver != "sqlite" {
		t.Fatalf("StorageDriver = %q, want sqlite", cfg.StorageDriver)
	}
	if cfg.AuthenticatedMode {
		t.Fatal("AuthenticatedMode defaulted to true")
	}
	if cfg.WorkbenchDir != "web/dist" {
		t.Fatalf("WorkbenchDir = %q, want web/dist", cfg.WorkbenchDir)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config should validate: %v", err)
	}
}

func TestLoadFromEnvNormalizesConfiguredValues(t *testing.T) {
	values := map[string]string{
		"DANNY_MODEL_ENDPOINT":     " https://model.example/v1 ",
		"DANNY_MODEL_NAME":         " local-model ",
		"DANNY_STORAGE_DRIVER":     " postgres ",
		"DANNY_DATABASE_URL":       " postgres://localhost/danny ",
		"DANNY_LISTEN_ADDRESS":     "127.0.0.1:9090",
		"DANNY_AUTHENTICATED":      "false",
		"DANNY_SCHEDULER_ENABLED":  "true",
		"DANNY_SCHEDULER_TIMEZONE": "Asia/Shanghai",
	}
	cfg, err := LoadFromEnv(func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	})
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}
	if cfg.ModelEndpoint != "https://model.example/v1" || cfg.ModelName != "local-model" {
		t.Fatalf("model values were not normalized: %#v", cfg)
	}
	if cfg.StorageDriver != "postgres" || cfg.DatabaseURL != "postgres://localhost/danny" {
		t.Fatalf("storage values were not normalized: %#v", cfg)
	}
	if !cfg.SchedulerEnabled || cfg.SchedulerTimezone != "Asia/Shanghai" {
		t.Fatalf("scheduler values were not parsed: %#v", cfg)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("configured values should validate: %v", err)
	}
}

func TestConfigValidateReportsStableSettingNames(t *testing.T) {
	cfg := Config{ListenAddress: "0.0.0.0:8080", StorageDriver: "unknown"}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want invalid configuration error")
	}
	message := err.Error()
	for _, setting := range []string{"DANNY_STORAGE_DRIVER", "DANNY_LISTEN_ADDRESS"} {
		if !strings.Contains(message, setting) {
			t.Fatalf("Validate() error %q does not mention %s", message, setting)
		}
	}
}

func TestConfigValidateRequiresDatabaseURLForPostgres(t *testing.T) {
	cfg := Config{ListenAddress: "127.0.0.1:8080", StorageDriver: "postgres"}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "DANNY_DATABASE_URL") {
		t.Fatalf("Validate() error = %v, want DANNY_DATABASE_URL error", err)
	}
}
