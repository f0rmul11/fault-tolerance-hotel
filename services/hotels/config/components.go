package config

import "time"

const ReviewEventsTopic = "review.rating.events.v1"

type Cache struct {
	SoftTTL          time.Duration `yaml:"soft_ttl"`
	HardTTL          time.Duration `yaml:"hard_ttl"`
	JitterPercentage float64       `yaml:"jitter_percentage" env:"CACHE_JITTER_PERCENTAGE"`
	RefreshTimeout   time.Duration `yaml:"refresh_timeout"`
	Negative         NegativeCache `yaml:"negative"`
}

// MaxHardTTL ограничивает возраст карточки с учётом положительного jitter.
func (c Cache) MaxHardTTL() time.Duration {
	return c.HardTTL + time.Duration(float64(c.HardTTL)*c.JitterPercentage)
}

type NegativeCache struct {
	NotFoundTTL          time.Duration `yaml:"not_found_ttl"`
	UnavailableTTL       time.Duration `yaml:"unavailable_ttl"`
	ResourceExhaustedTTL time.Duration `yaml:"resource_exhausted_ttl"`
}

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

type Postgres struct {
	MaxConnections     int32         `yaml:"max_connections"`
	MinConnections     int32         `yaml:"min_connections"`
	MaxIdleConnections int32         `yaml:"max_idle_connections"`
	MaxConnLifetime    time.Duration `yaml:"max_conn_lifetime"`
}
