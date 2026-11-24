package config

import (
	"time"

	"review/internal/pkg/outbox"
)

const RatingEventsTopic = "review.rating.events.v1"

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

type Postgres struct {
	MaxConnections     int32         `yaml:"max_connections"`
	MinConnections     int32         `yaml:"min_connections"`
	MaxIdleConnections int32         `yaml:"max_idle_connections"`
	MaxConnLifetime    time.Duration `yaml:"max_conn_lifetime"`
}
