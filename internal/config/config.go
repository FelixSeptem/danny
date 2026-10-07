package config

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/FelixSeptem/danny/internal/security"
)

// Config contains the validated process configuration for the local foundation.
type Config struct {
	ModelEndpoint     string
	ModelName         string
	BaymaxVersion     string
	SteleVersion      string
	StorageDriver     string
	DatabaseURL       string
	DataDir           string
	DocumentsDir      string
	ArtifactsDir      string
	FixtureDir        string
	ListenAddress     string
	AuthenticatedMode bool
	SchedulerEnabled  bool
	SchedulerTimezone string
	DockerProfile     string
}

// LoadFromEnv parses DANNY_* variables and applies documented local defaults.
// It does not read files, bind sockets, start workers, or contact external services.
func LoadFromEnv(lookup func(string) (string, bool)) (Config, error) {
	if lookup == nil {
		lookup = func(string) (string, bool) { return "", false }
	}
	get := func(key, fallback string) string {
		if value, ok := lookup(key); ok {
			return strings.TrimSpace(value)
		}
		return fallback
	}
	parseBool := func(key string, fallback bool) (bool, error) {
		value, ok := lookup(key)
		if !ok || strings.TrimSpace(value) == "" {
			return fallback, nil
		}
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return false, fmt.Errorf("%s must be true or false", key)
		}
		return parsed, nil
	}
	authenticated, err := parseBool("DANNY_AUTHENTICATED", false)
	if err != nil {
		return Config{}, err
	}
	schedulerEnabled, err := parseBool("DANNY_SCHEDULER_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	return Config{
		ModelEndpoint:     get("DANNY_MODEL_ENDPOINT", ""),
		ModelName:         get("DANNY_MODEL_NAME", ""),
		BaymaxVersion:     get("DANNY_BAYMAX_VERSION", "v0.0.0-20261003094000-553e07625efb"),
		SteleVersion:      get("DANNY_STELE_VERSION", "v0.0.0-20261007033528-a556ac0ddacc"),
		StorageDriver:     strings.ToLower(get("DANNY_STORAGE_DRIVER", "sqlite")),
		DatabaseURL:       get("DANNY_DATABASE_URL", "data/danny.db"),
		DataDir:           get("DANNY_DATA_DIR", "data"),
		DocumentsDir:      get("DANNY_DOCUMENTS_DIR", "data/documents"),
		ArtifactsDir:      get("DANNY_ARTIFACTS_DIR", "data/artifacts"),
		FixtureDir:        get("DANNY_FIXTURE_DIR", "testdata/fixtures"),
		ListenAddress:     get("DANNY_LISTEN_ADDRESS", "127.0.0.1:8080"),
		AuthenticatedMode: authenticated,
		SchedulerEnabled:  schedulerEnabled,
		SchedulerTimezone: get("DANNY_SCHEDULER_TIMEZONE", "UTC"),
		DockerProfile:     get("DANNY_DOCKER_PROFILE", "local"),
	}, nil
}

// Validate applies cross-field rules and returns stable environment setting names.
func (c Config) Validate() error {
	var problems []string
	if c.StorageDriver != "sqlite" && c.StorageDriver != "postgres" {
		problems = append(problems, "DANNY_STORAGE_DRIVER must be sqlite or postgres")
	}
	if c.StorageDriver == "postgres" && strings.TrimSpace(c.DatabaseURL) == "" {
		problems = append(problems, "DANNY_DATABASE_URL is required when DANNY_STORAGE_DRIVER is postgres")
	}
	if c.DatabaseURL != "" && c.StorageDriver == "postgres" {
		if parsed, err := url.Parse(c.DatabaseURL); err != nil || parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
			problems = append(problems, "DANNY_DATABASE_URL must use postgres or postgresql scheme")
		}
	}
	if err := security.ValidateListener(c.ListenAddress, c.AuthenticatedMode); err != nil {
		problems = append(problems, "DANNY_LISTEN_ADDRESS: "+err.Error())
	}
	if c.SchedulerTimezone == "" {
		problems = append(problems, "DANNY_SCHEDULER_TIMEZONE must not be empty")
	}
	if len(problems) > 0 {
		return fmt.Errorf("invalid configuration: %s", strings.Join(problems, "; "))
	}
	return nil
}
