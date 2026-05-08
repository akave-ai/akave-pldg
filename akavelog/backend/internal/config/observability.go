package config

import (
	"fmt"
	"time"
)

// @dev observability - the ability to monitor, log, and understand what's happening in your application when it's running.
// @dev This contain Logs, New Relic(performance), Health checks, Distributed tracing(shows which microservice caused the issue)

type ObservabilityConfig struct {
	ServiceName  string             `koanf:"service_name" validate:"required"`
	Environment  string             `koanf:"environment" validate:"required"`
	Logging      LoggingConfig      `koanf:"logging" validate:"required"`
	NewRelic     NewRelicConfig     `koanf:"new_relic" validate:"required"`
	HealthChecks HealthChecksConfig `koanf:"health_checks" validate:"required"`
}

type LoggingConfig struct {
	Level              string        `koanf:"level" validate:"required"`
	Format             string        `koanf:"format" validate:"required"`
	SlowQueryThreshold time.Duration `koanf:"slow_query_threshold"`
}

type NewRelicConfig struct {
	LicenseKey                string `koanf:"license_key"` // empty = New Relic disabled
	AppLogForwardingEnabled   bool   `koanf:"app_log_forwarding_enabled"`
	DistributedTracingEnabled bool   `koanf:"distributed_tracing_enabled"`
	DebugLogging              bool   `koanf:"debug_logging"`
}

type HealthChecksConfig struct {
	Enabled  bool          `koanf:"enabled"`
	Interval time.Duration `koanf:"interval" validate:"min=1s"`
	Timeout  time.Duration `koanf:"timeout" validate:"min=1s"`
	Checks   []string      `koanf:"checks"`
}

func DefaultObservabilityConfig() *ObservabilityConfig {
	return &ObservabilityConfig{
		ServiceName: "akavelog",
		Environment: "development",
		Logging: LoggingConfig{
			Level:              "info",
			Format:             "json",
			SlowQueryThreshold: 100 * time.Millisecond,
		},
		NewRelic: NewRelicConfig{
			LicenseKey:                "",
			AppLogForwardingEnabled:   true,
			DistributedTracingEnabled: true,
			DebugLogging:              false, // Disabled by default to avoid mixed log formats
		},
		HealthChecks: HealthChecksConfig{
			Enabled:  true,
			Interval: 100 * time.Millisecond,
			Timeout:  100 * time.Millisecond,
			Checks:   []string{"db", "redis"},
		},
	}
}

func (c *ObservabilityConfig) Validate() error {
	if c.ServiceName == "" {
		return fmt.Errorf("service name is required")
	}

	if c.Logging.SlowQueryThreshold < 0 {
		return fmt.Errorf("SlowQueryThreshold should non-negative")
	}

	return nil
}

func (c *ObservabilityConfig) GetLogLevel() string {
	switch c.Environment {
	case "production":
		if c.Logging.Level == "" {
			return "info"
		}
	case "development":
		if c.Logging.Level == "" {
			return "info"
		}
	}

	return c.Logging.Level
}

func (c *ObservabilityConfig) IsProduction() bool {
	if c.Environment == "production" {
		return true
	}
	return false
}
