package config

import (
	"strings"
	"testing"
)

func TestContainerListenIsOptInAndDefaultPolicyIsUnchanged(t *testing.T) {
	bare := Config{
		ListenAddress:     "0.0.0.0:8080",
		StorageDriver:     "sqlite",
		SchedulerTimezone: "UTC",
	}
	if err := bare.Validate(); err == nil || !strings.Contains(err.Error(), "non-loopback") {
		t.Fatalf("default profile must still reject a non-loopback listener, got %v", err)
	}

	container := Config{
		ListenAddress:     "0.0.0.0:8080",
		StorageDriver:     "sqlite",
		SchedulerTimezone: "UTC",
		ContainerListen:   true,
	}
	if err := container.Validate(); err != nil {
		t.Fatalf("container wildcard listener should validate: %v", err)
	}
	if container.AuthenticatedMode {
		t.Fatal("container listener must not enable authenticated mode")
	}

	concrete := container
	concrete.ListenAddress = "192.168.1.10:8080"
	if err := concrete.Validate(); err == nil || !strings.Contains(err.Error(), "wildcard") {
		t.Fatalf("concrete IP must be rejected in container mode, got %v", err)
	}
}

func TestLoadFromEnvParsesContainerListenAndModelCredentials(t *testing.T) {
	values := map[string]string{
		"DANNY_CONTAINER_LISTEN": "true",
		"DANNY_LISTEN_ADDRESS":   "0.0.0.0:8080",
		"DANNY_MODEL_ENDPOINT":   "https://model.example/v1",
		"DANNY_MODEL_NAME":       "local-model",
		"DANNY_MODEL_API_KEY":    "env-only-secret",
	}
	cfg, err := LoadFromEnv(func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	})
	if err != nil {
		t.Fatalf("LoadFromEnv() error = %v", err)
	}
	if !cfg.ContainerListen {
		t.Fatal("DANNY_CONTAINER_LISTEN was not parsed")
	}
	if cfg.AuthenticatedMode {
		t.Fatal("container profile must not turn on authenticated mode")
	}
	if cfg.ModelAPIKey != "env-only-secret" {
		t.Fatalf("model credentials were not injected through the environment: %q", cfg.ModelAPIKey)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("container config should validate: %v", err)
	}
}
