package config

import (
	"fmt"
	"time"

	"github.com/kelseyhightower/envconfig"
)

// Config represents the application configuration
type Config struct {
	Server   ServerConfig   `envconfig:"SERVER"`
	Storage  StorageConfig  `envconfig:"STORAGE"`
	Pruning  PruningConfig  `envconfig:"PRUNING"`
	Metrics  MetricsConfig  `envconfig:"METRICS"`
	Security SecurityConfig `envconfig:"SECURITY"`
}

// ServerConfig contains gRPC server configuration
type ServerConfig struct {
	Port             int  `envconfig:"PORT" default:"8080"`
	EnableReflection bool `envconfig:"ENABLE_REFLECTION" default:"false"`
}

// StorageConfig contains Cloud Storage configuration
type StorageConfig struct {
	BucketName string `envconfig:"BUCKET_NAME" required:"true"`
	ProjectID  string `envconfig:"PROJECT_ID" required:"true"`
}

// PruningConfig contains cache pruning configuration
type PruningConfig struct {
	MaxCacheSizeGB int           `envconfig:"MAX_CACHE_SIZE_GB" default:"1000"`
	IntervalHours  time.Duration `envconfig:"INTERVAL_HOURS" default:"24"`
	RetentionDays  int           `envconfig:"RETENTION_DAYS" default:"30"`
	EnablePruning  bool          `envconfig:"ENABLE_PRUNING" default:"true"`
}

// MetricsConfig contains metrics server configuration
type MetricsConfig struct {
	Port int `envconfig:"PORT" default:"9090"`
}

// SecurityConfig contains security-related configuration
type SecurityConfig struct {
	EnableTLS       bool   `envconfig:"ENABLE_TLS" default:"true"`
	CertPath        string `envconfig:"CERT_PATH" default:"/etc/ssl/certs/tls.crt"`
	KeyPath         string `envconfig:"KEY_PATH" default:"/etc/ssl/private/tls.key"`
	RequireAuth     bool   `envconfig:"REQUIRE_AUTH" default:"true"`
	AllowedProjects string `envconfig:"ALLOWED_PROJECTS"`
}

// Load reads configuration from environment variables
func Load() (*Config, error) {
	var cfg Config
	if err := envconfig.Process("CACHE", &cfg); err != nil {
		return nil, fmt.Errorf("failed to process environment config: %w", err)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &cfg, nil
}

// Validate ensures the configuration is valid
func (c *Config) Validate() error {
	if c.Storage.BucketName == "" {
		return fmt.Errorf("storage bucket name is required")
	}

	if c.Storage.ProjectID == "" {
		return fmt.Errorf("storage project ID is required")
	}

	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", c.Server.Port)
	}

	if c.Metrics.Port <= 0 || c.Metrics.Port > 65535 {
		return fmt.Errorf("invalid metrics port: %d", c.Metrics.Port)
	}

	if c.Pruning.MaxCacheSizeGB <= 0 {
		return fmt.Errorf("max cache size must be positive")
	}

	if c.Pruning.RetentionDays <= 0 {
		return fmt.Errorf("retention days must be positive")
	}

	return nil
}
