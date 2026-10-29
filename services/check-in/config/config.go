package config

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"
	_ "time/tzdata"

	"check-in/internal/pkg/telemetry"

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
	Telemetry     telemetry.Config  `yaml:"telemetry"`
	Timezone      string            `yaml:"timezone" env:"CHECKIN_TIMEZONE"`
	Inbox         Inbox             `yaml:"inbox"`
	ConsumerGroup string            `yaml:"consumer_group" env:"KAFKA_CONSUMER_GROUP"`
	Postgres      Postgres          `yaml:"postgres"`
	Outbox        Outbox            `yaml:"outbox"`
	KafkaBrokers  []string          `yaml:"kafka_brokers" env:"KAFKA_BROKERS" env-separator:","`
	Targets       map[string]string `yaml:"service"`
	GrpcServer    GrpcServer        `yaml:"grpc_server"`
	HttpServer    HttpServer        `yaml:"http_server"`
	Graceful      Graceful          `yaml:"graceful"`
	PostgresDSN   string            `yaml:"postgres_dsn" env:"POSTGRES_DSN"`
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

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) Validate() error {
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		return fmt.Errorf("некорректный часовой пояс: %w", err)
	}
	if c.ConsumerGroup == "" {
		return fmt.Errorf("не указана Kafka consumer group")
	}
	if c.Inbox.Config.MessagesLimit < 1 ||
		c.Inbox.Config.MaxErrCountForMessage < 1 ||
		c.Inbox.Config.NormalMessagesPollInterval <= 0 ||
		c.Inbox.Config.ErrorMessagesPollInterval <= 0 ||
		c.InboxConfig(BookingEventsTopic).BatchSize < 1 {
		return fmt.Errorf("некорректные настройки inbox")
	}

	if c.PostgresDSN == "" {
		return fmt.Errorf("POSTGRES_DSN обязателен")
	}

	if len(c.KafkaBrokers) == 0 {
		return fmt.Errorf("KAFKA_BROKERS обязателен")
	}

	if err := c.Postgres.Validate(); err != nil {
		return err
	}

	if c.Outbox.Limits.LockedKeysLimit < 1 ||
		c.Outbox.Limits.MessagesLimit < 1 ||
		c.Outbox.Limits.MaxErrCountForMessage < 1 {
		return fmt.Errorf("лимиты outbox должны быть положительными")
	}

	handler := c.OutboxConfig(CheckinEventsTopic)
	if handler.BatchSize < 1 || handler.Worker.Concurrency < 1 || handler.Worker.Interval <= 0 {
		return fmt.Errorf("некорректные настройки обработчика outbox")
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

	if c.Graceful.Delay < 0 || c.Graceful.Delay >= c.Graceful.Timeout {
		return fmt.Errorf("graceful delay must be non-negative and less than timeout")
	}
	if c.Graceful.Timeout <= 0 {
		return fmt.Errorf("graceful timeout must be positive")
	}

	return nil
}

func (c *Config) OutboxConfig(topic string) OutboxHandler {
	return c.Outbox.Topics[topic]
}

func (c *Config) InboxConfig(topic string) InboxHandler {
	return c.Inbox.Topics[topic]
}
