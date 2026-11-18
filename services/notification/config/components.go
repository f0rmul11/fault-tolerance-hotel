package config

import "time"

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

const (
	BookingEventsTopic = "booking.events.v1"
	BookingRetry1Topic = "notification.booking.retry.1.v1"
	BookingRetry2Topic = "notification.booking.retry.2.v1"
	BookingDLQTopic    = "notification.booking.dlq.v1"
)

type RetryStage struct {
	Group string        `yaml:"group"`
	Delay time.Duration `yaml:"delay"`
}
