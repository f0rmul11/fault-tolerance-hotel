package config

import (
	"fmt"
	"time"

	"check-in/internal/pkg/inbox"
	"check-in/internal/pkg/outbox"
)

type GrpcServer struct {
	Port              int           `yaml:"port" env:"GRPC_PORT"`
	MaxConnectionIdle time.Duration `yaml:"max_connection_idle"`
	MaxConnectionAge  time.Duration `yaml:"max_connection_age"`
	Timeout           time.Duration `yaml:"timeout"`
	Time              time.Duration `yaml:"time"`
}

type HttpServer struct {
	Port      int `yaml:"port" env:"HTTP_PORT"`
	AdminPort int `yaml:"admin_port" env:"ADMIN_HTTP_PORT"`
}

type Graceful struct {
	Delay   time.Duration `yaml:"delay" env:"GRACEFUL_DELAY"`
	Timeout time.Duration `yaml:"timeout" env:"GRACEFUL_TIMEOUT"`
}

const CheckinEventsTopic = "checkin.events.v1"

type Postgres struct {
	MaxConnections     int32         `yaml:"max_connections"`
	MinConnections     int32         `yaml:"min_connections"`
	MaxIdleConnections int32         `yaml:"max_idle_connections"`
	MaxConnLifetime    time.Duration `yaml:"max_conn_lifetime"`
}

type Outbox struct {
	Limits outbox.Config            `yaml:"limits"`
	Topics map[string]OutboxHandler `yaml:"topics"`
}

type OutboxHandler struct {
	BatchSize int    `yaml:"batch_size"`
	Worker    Worker `yaml:"worker"`
}

type Worker struct {
	Interval    time.Duration `yaml:"interval"`
	Concurrency int           `yaml:"concurrency"`
}

// Validate проверяет границы параметров пула без создания соединений.
func (p Postgres) Validate() error {
	if p.MaxConnections < 1 {
		return fmt.Errorf("максимальное число соединений PostgreSQL должно быть положительным")
	}
	if p.MinConnections < 0 || p.MinConnections > p.MaxConnections {
		return fmt.Errorf("минимальное число соединений PostgreSQL должно быть от 0 до максимального")
	}
	if p.MaxIdleConnections < 0 || p.MaxIdleConnections > p.MaxConnections {
		return fmt.Errorf("число свободных соединений PostgreSQL должно быть от 0 до максимального")
	}
	if p.MaxConnLifetime <= 0 {
		return fmt.Errorf("срок жизни соединения PostgreSQL должен быть положительным")
	}
	return nil
}

const BookingEventsTopic = "booking.events.v1"

type Inbox struct {
	Config inbox.Config            `yaml:"config"`
	Topics map[string]InboxHandler `yaml:"topics"`
}
type InboxHandler struct {
	BatchSize int `yaml:"batch_size"`
}
