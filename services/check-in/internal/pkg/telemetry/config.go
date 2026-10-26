package telemetry

import (
	"net/url"
	"strings"
	"time"
)

// Config contains the process identity and the shared OTLP gRPC connection settings.
type Config struct {
	MetricsEnabled   bool          `yaml:"metrics_enabled" env:"TELEMETRY_METRICS_ENABLED"`
	MetricsInterval  time.Duration `yaml:"metrics_interval" env:"TELEMETRY_METRICS_INTERVAL"`
	LogsEnabled      bool          `yaml:"logs_enabled" env:"TELEMETRY_LOGS_ENABLED"`
	LogLevel         string        `yaml:"log_level" env:"LOG_LEVEL"`
	Enabled          bool          `yaml:"enabled" env:"TELEMETRY_ENABLED" env-default:"false"`
	ServiceName      string        `yaml:"service_name" env:"OTEL_SERVICE_NAME"`
	ServiceNamespace string        `yaml:"service_namespace"`
	ServiceVersion   string        `yaml:"service_version"`
	TracerName       string        `yaml:"tracer_name"`
	OTLPEndpoint     string        `yaml:"otlp_endpoint" env:"OTEL_EXPORTER_OTLP_ENDPOINT"`
	OTLPInsecure     bool          `yaml:"otlp_insecure" env:"OTEL_EXPORTER_OTLP_INSECURE"`
}

func normalizeConfig(config Config) Config {
	if config.MetricsInterval <= 0 {
		config.MetricsInterval = 15 * time.Second
	}
	if config.ServiceNamespace == "" {
		config.ServiceNamespace = "hotelflow"
	}
	if config.TracerName == "" {
		config.TracerName = "hotelflow/" + config.ServiceName
	}
	if config.OTLPEndpoint == "" {
		config.OTLPEndpoint = "otel-collector:4317"
	}
	return config
}

func normalizeGRPCEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if parsed, err := url.Parse(endpoint); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		return parsed.Host
	}
	return endpoint
}
