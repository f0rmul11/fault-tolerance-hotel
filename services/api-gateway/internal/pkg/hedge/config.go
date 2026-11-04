package hedge

import (
	"time"

	"github.com/samber/lo"
)

type Config struct {
	Default  DefaultConfig   `yaml:"__default__"`
	Handlers []HandlerConfig `yaml:"handlers"`
}

func (c *Config) handlersConfig() map[string]HandlerConfig {
	handlersConfig := make(map[string]HandlerConfig, len(c.Handlers))
	for _, config := range c.Handlers {
		handlersConfig[config.Method] = config
	}

	return handlersConfig
}

type DefaultConfig struct {
	MainConfig                `yaml:",inline"`
	SeparatePerHandlerEnabled *bool `yaml:"separate_hr_per_handler_enabled"`
}

type HandlerConfig struct {
	// Имя метода
	Method     string `yaml:"method"`
	MainConfig `yaml:",inline"`
}

type MainConfig struct {
	// Включена ли фича. У handler значение задаётся явно и не наследует default.
	Enabled bool `yaml:"enabled"`
	// Размер окна подсчета времен ответов
	WindowSize int `yaml:"window_size"`
	// До запуска дублей накопить полное окно обычных ответов.
	Warmup bool `yaml:"warmup"`
	// Перцентиль окна для расчёта задержки запуска дубля
	Percentile float64 `yaml:"percentile"`
	// Офсет взятия latency для оптимизации нужного перцентиля
	PercentileOffset float64 `yaml:"percentile_offset"`
	// Бюджет hedge запросов относительно общего числа
	HedgeBudget float64 `yaml:"budget"`
	// период сброса счетчиков
	ResetPeriod time.Duration `yaml:"reset_period"`
}

func withDefaults(cfg Config) Config {
	cfg.Default.SeparatePerHandlerEnabled = overrideIfZero(cfg.Default.SeparatePerHandlerEnabled, lo.ToPtr(true))
	cfg.Default.MainConfig = defaultConfig(cfg.Default.MainConfig)

	for idx, h := range cfg.Handlers {
		cfg.Handlers[idx].MainConfig = enrich(h.MainConfig, cfg.Default.MainConfig)
	}

	return cfg
}

func enrich(cfg, enrichment MainConfig) MainConfig {
	cfg.WindowSize = overrideIfZero(cfg.WindowSize, enrichment.WindowSize)
	cfg.Percentile = overrideIfZero(cfg.Percentile, enrichment.Percentile)
	cfg.HedgeBudget = overrideIfZero(cfg.HedgeBudget, enrichment.HedgeBudget)
	cfg.PercentileOffset = overrideIfZero(cfg.PercentileOffset, enrichment.PercentileOffset)
	cfg.ResetPeriod = overrideIfZero(cfg.ResetPeriod, enrichment.ResetPeriod)
	return cfg
}

func defaultConfig(cfg MainConfig) MainConfig {
	defaultCfg := MainConfig{
		Enabled:          false,
		WindowSize:       1000,
		Percentile:       0.99, // p99
		HedgeBudget:      0.1,  // 10%
		PercentileOffset: 0.0001,
		ResetPeriod:      time.Second,
	}

	return enrich(cfg, defaultCfg)
}
