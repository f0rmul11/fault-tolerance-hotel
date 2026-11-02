package circuit

import (
	"time"

	"github.com/samber/lo"
	"google.golang.org/grpc/codes"
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
	SeparatePerHandlerEnabled *bool `yaml:"separate_circuit_per_handler_enabled"`
}

type HandlerConfig struct {
	// Имя метода
	Method     string `yaml:"method"`
	MainConfig `yaml:",inline"`
}

type MainConfig struct {
	// Активирован ли circuit breaker для этого метода
	Enabled bool `yaml:"enabled"`
	// Время в открытом состоянии до перехода в полуоткрытое
	// Максимальное количество запросов в полуоткрытом состоянии
	MaxRequests uint32 `yaml:"max_requests"`
	// Время в открытом состоянии до перехода в полуоткрытое
	OpenTimeout time.Duration `yaml:"timeout"`
	// Время нахождения в closed
	Interval time.Duration `yaml:"interval"`
	// Длина каждого бакета временного окна
	BucketPeriod time.Duration `yaml:"bucket_period"`
	// Порог срабатывания последовательный (количество ошибок для перехода в открытое состояние)
	ThresholdConsecutive uint32 `yaml:"threshold_consecutive"`
	// Порог срабатывания в процентах (количество ошибок для перехода в открытое состояние)
	ThresholdPercentage uint32 `yaml:"threshold_percentage"`
	// Коды ошибок для реагирования
	FailureCodes []string `yaml:"failure_codes"`
}

// failureCodeSet Преобразует список строковых кодов в множество кодов
func (c *MainConfig) failureCodeSet() map[codes.Code]struct{} {
	set := make(map[codes.Code]struct{}, len(c.FailureCodes))
	for _, codeStr := range c.FailureCodes {
		if code, ok := failureCode(codeStr); ok {
			set[code] = struct{}{}
		}
	}
	return set
}

func withDefaults(cfg Config) Config {
	cfg.Default.SeparatePerHandlerEnabled = overrideIfZero(cfg.Default.SeparatePerHandlerEnabled, lo.ToPtr(true))
	cfg.Default.MainConfig = defaultConfig(cfg.Default.MainConfig)

	for idx, h := range cfg.Handlers {
		cfg.Handlers[idx].MainConfig = enrich(h.MainConfig, cfg.Default.MainConfig)
		// Handler entries explicitly select enabled/disabled, including false.
		cfg.Handlers[idx].Enabled = h.Enabled
	}

	return cfg
}

func enrich(cfg, enrichment MainConfig) MainConfig {
	cfg.Enabled = overrideIfZero(cfg.Enabled, enrichment.Enabled)
	cfg.ThresholdConsecutive = overrideIfZero(cfg.ThresholdConsecutive, enrichment.ThresholdConsecutive)
	cfg.ThresholdPercentage = overrideIfZero(cfg.ThresholdPercentage, enrichment.ThresholdPercentage)
	cfg.MaxRequests = overrideIfZero(cfg.MaxRequests, enrichment.MaxRequests)
	cfg.OpenTimeout = overrideIfZero(cfg.OpenTimeout, enrichment.OpenTimeout)
	cfg.Interval = overrideIfZero(cfg.Interval, enrichment.Interval)
	cfg.BucketPeriod = overrideIfZero(cfg.BucketPeriod, enrichment.BucketPeriod)
	cfg.FailureCodes = overrideIfNilSlice(cfg.FailureCodes, enrichment.FailureCodes)
	return cfg
}

func defaultConfig(cfg MainConfig) MainConfig {
	defaultCfg := MainConfig{
		Enabled:              false,
		MaxRequests:          10,
		OpenTimeout:          10 * time.Second,
		Interval:             30 * time.Second,
		BucketPeriod:         2 * time.Second,
		ThresholdConsecutive: 5,
		ThresholdPercentage:  25,
		FailureCodes:         lo.Keys(errorCodeMap),
	}

	return enrich(cfg, defaultCfg)
}
