package ratelimit

import (
	"time"
)

// Config содержит упорядоченные правила: применяется первое совпадение.
// Один bucket общий для всех клиентов и методов правила в одном процессе.
// Пустой список правил отключает ограничение.
type Config struct {
	Rules []MainConfig `yaml:"rules"`
}

type DefaultConfig struct {
	MainConfig `yaml:",inline"`
}

type MainConfig struct {
	// Clients — имена вызывающих сервисов; пустой список подходит всем.
	// Неизвестный клиент имеет имя unknown, но не совпадает с другим именем.
	Clients []string `yaml:"clients"`
	// Handlers — полные gRPC methods; пустой список подходит всем.
	Handlers []string `yaml:"handlers"`
	// Limit — скорость пополнения в токенах/секунду; 0 запрещает все запросы.
	Limit float64 `yaml:"limit"`
	// Burst — ёмкость bucket, а не дополнительный RPS. 0 означает ceil(Limit).
	Burst int `yaml:"burst"`
	// Timeout — максимальное ожидание токена; 0 означает немедленный отказ.
	Timeout time.Duration `yaml:"timeout"`
}
