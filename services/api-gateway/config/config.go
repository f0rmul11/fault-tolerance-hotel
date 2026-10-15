package config

import (
	"fmt"
	"log"
	"os"
	"sync"

	"api-gateway/internal/pkg/rtconfig"

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
	RuntimeConfig rtconfig.Config   `yaml:"runtime_config"`
	Targets       map[string]string `yaml:"service"`
	GrpcServer    GrpcServer        `yaml:"grpc_server"`
	HttpServer    HttpServer        `yaml:"http_server"`
	Graceful      Graceful          `yaml:"graceful"`
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
	if c.Graceful.Timeout <= 0 {
		return fmt.Errorf("graceful timeout must be positive")
	}
	return nil
}
