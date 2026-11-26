package config

import (
	"fmt"
	"log"
	"os"
	"sync"

	"hotels/internal/pkg/circuit"
	"hotels/internal/pkg/rtconfig"
	"hotels/internal/pkg/telemetry"

	"github.com/ilyakaznacheev/cleanenv"
)

var (
	instance *Config
	once     sync.Once
)

func Instance() *Config {
	once.Do(func() {
		var err error
		instance, err = Read()
		if err != nil {
			log.Fatalf("read config error: %s", err)
		}
	})
	return instance
}

type Config struct {
	KafkaBrokers    []string          `yaml:"kafka_brokers" env:"KAFKA_BROKERS" env-separator:","`
	ConsumerGroup   string            `yaml:"consumer_group" env:"CONSUMER_GROUP"`
	Cache           Cache             `yaml:"cache"`
	ValkeyAddresses []string          `yaml:"valkey_addresses" env:"VALKEY_ADDRESSES" env-separator:","`
	Circuit         circuit.Config    `yaml:"circuit"`
	RuntimeConfig   rtconfig.Config   `yaml:"runtime_config"`
	Telemetry       telemetry.Config  `yaml:"telemetry"`
	Postgres        Postgres          `yaml:"postgres"`
	PostgresDSN     string            `yaml:"postgres_dsn" env:"POSTGRES_DSN"`
	Targets         map[string]string `yaml:"service" env:"SERVICE_TARGETS"`
	GrpcServer      GrpcServer        `yaml:"grpc_server"`
	HttpServer      HttpServer        `yaml:"http_server"`
	Graceful        Graceful          `yaml:"graceful"`
}

// Read загружает конфигурацию из YAML и применяет переменные окружения.
func Read() (*Config, error) {
	path := os.Getenv("CONFIG_PATH")
	if path == "" {
		path = "config/config.yaml"
	}

	cfg := &Config{}
	if err := cleanenv.ReadConfig(path, cfg); err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	cfg.RuntimeConfig.ResolveSchemaPath(path)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) Validate() error {
	if len(c.KafkaBrokers) == 0 || c.ConsumerGroup == "" {
		return fmt.Errorf("не указаны Kafka brokers или consumer group")
	}
	if c.Cache.SoftTTL <= 0 || c.Cache.HardTTL <= c.Cache.SoftTTL {
		return fmt.Errorf("TTL кеша должны удовлетворять 0 < soft_ttl < hard_ttl")
	}
	if c.Cache.RefreshTimeout <= 0 || c.Cache.RefreshTimeout >= c.Cache.HardTTL {
		return fmt.Errorf("refresh_timeout должен быть положительным и меньше hard_ttl")
	}

	negative := c.Cache.Negative
	if negative.NotFoundTTL < 0 || negative.UnavailableTTL < 0 || negative.ResourceExhaustedTTL < 0 {
		return fmt.Errorf("TTL в cache.negative должны быть неотрицательными")
	}

	for name, port := range map[string]int{
		"HTTP_PORT":       c.HttpServer.Port,
		"GRPC_PORT":       c.GrpcServer.Port,
		"ADMIN_HTTP_PORT": c.HttpServer.AdminPort,
	} {
		if port < 1 || port > 65535 {
			return fmt.Errorf("%s must be between 1 and 65535", name)
		}
	}
	if c.HttpServer.Port == c.GrpcServer.Port {
		return fmt.Errorf("HTTP_PORT and GRPC_PORT must differ")
	}
	if c.HttpServer.AdminPort == c.HttpServer.Port || c.HttpServer.AdminPort == c.GrpcServer.Port {
		return fmt.Errorf("ADMIN_HTTP_PORT must differ from HTTP_PORT and GRPC_PORT")
	}
	if err := c.RuntimeConfig.Validate(); err != nil {
		return err
	}
	if c.Graceful.Delay < 0 || c.Graceful.Delay >= c.Graceful.Timeout {
		return fmt.Errorf("graceful delay must be non-negative and less than timeout")
	}
	if c.Graceful.Timeout <= 0 {
		return fmt.Errorf("graceful timeout must be positive")
	}
	return nil
}
