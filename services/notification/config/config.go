package config

import (
	"fmt"
	"log"
	"os"
	"sync"

	"notification/internal/pkg/telemetry"

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
	Telemetry     telemetry.Config      `yaml:"telemetry"`
	KafkaBrokers  []string              `yaml:"kafka_brokers" env:"KAFKA_BROKERS" env-separator:","`
	ConsumerGroup string                `yaml:"consumer_group" env:"KAFKA_CONSUMER_GROUP"`
	Retry         map[string]RetryStage `yaml:"retry"`
	Targets       map[string]string     `yaml:"service"`
	GrpcServer    GrpcServer            `yaml:"grpc_server"`
	HttpServer    HttpServer            `yaml:"http_server"`
	Graceful      Graceful              `yaml:"graceful"`
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

	return cfg, nil
}
