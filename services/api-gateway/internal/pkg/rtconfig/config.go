package rtconfig

import (
	"fmt"
	"path/filepath"
	"time"
)

// Config задаёт статические параметры runtime-клиента для встраивания в конфиг сервиса.
// Prefix задаётся сервисом явно: библиотека не выбирает сервис или окружение.
type Config struct {
	Endpoints       []string      `yaml:"endpoints" env:"RTCONFIG_ENDPOINTS" env-default:"http://127.0.0.1:2379" env-separator:","`
	Prefix          string        `yaml:"prefix" env:"RTCONFIG_PREFIX"`
	SchemaPath      string        `yaml:"schema_path" env:"RTCONFIG_SCHEMA_PATH" env-default:"rtconfig.yaml"`
	StartupTimeout  time.Duration `yaml:"startup_timeout" env:"RTCONFIG_STARTUP_TIMEOUT" env-default:"5s"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout" env:"RTCONFIG_SHUTDOWN_TIMEOUT" env-default:"3s"`
}

// ResolveSchemaPath разрешает относительный путь рядом со статическим YAML сервиса.
// Вызывается один раз после загрузки настроек из файла и окружения.
func (c *Config) ResolveSchemaPath(configPath string) {
	if c.SchemaPath != "" && !filepath.IsAbs(c.SchemaPath) {
		c.SchemaPath = filepath.Join(filepath.Dir(configPath), c.SchemaPath)
	}
}

func (c Config) Validate() error {
	if c.SchemaPath == "" {
		return fmt.Errorf("rtconfig: schema path is required")
	}
	if c.StartupTimeout <= 0 || c.ShutdownTimeout <= 0 {
		return fmt.Errorf("rtconfig: startup and shutdown timeouts must be positive")
	}
	_, err := (Options{
		Endpoints: c.Endpoints,
		Prefix:    c.Prefix,
	}).normalized()
	return err
}
