package retry

import (
	"slices"
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
	SeparatePerHandlerEnabled *bool `yaml:"separate_retry_per_handler_enabled"`
}

type HandlerConfig struct {
	// Имя метода
	Method     string `yaml:"method"`
	MainConfig `yaml:",inline"`
}

type MainConfig struct {
	// Включена ли фича
	Enabled *bool `yaml:"enabled"`

	// MaxAttempts максимальное кол-во запросов, включая первый исходный
	MaxAttempts int `yaml:"max_attempts"`

	// Пауза перед повтором: min(initial * multiplier^n, max) * U[0.8, 1.2).
	// Первая попытка идёт сразу. MaxBackoff ограничивает базу до jitter.
	InitialBackoff    time.Duration `yaml:"initial_backoff"`
	MaxBackoff        time.Duration `yaml:"max_backoff"`
	BackoffMultiplier float64       `yaml:"backoff_multiplier"`

	// Конфигурация троттлера
	Throttler ThrottlerConfig `yaml:"throttler"`

	// RetryableStatusCodes коды ошибок, который можно учитывать для retry
	RetryableStatusCodes []string `yaml:"retryable_codes"`
}

// failureCodeSet Преобразует список строковых кодов в множество кодов
func (c *MainConfig) retryableCodesSet() map[codes.Code]struct{} {
	set := make(map[codes.Code]struct{}, len(c.RetryableStatusCodes))
	for _, codeStr := range c.RetryableStatusCodes {
		if code, ok := retryableCodeMap[codeStr]; ok {
			set[code] = struct{}{}
		}
	}
	return set
}

type ThrottlerConfig struct {
	// Enabled
	Enabled *bool `yaml:"throttling_enabled"`
	// MaxTokens бюджет токенов для повторных попыток
	MaxTokens float64 `yaml:"throttle_max_tokens"`
	// TokenRatio величина, которую добавляем при успешном запросе
	TokenRatio float64 `yaml:"throttle_token_ratio"`
}

func withDefaults(cfg Config) Config {
	cfg.Handlers = slices.Clone(cfg.Handlers)
	cfg.Default.SeparatePerHandlerEnabled = lo.ToPtr(*overrideIfZero(cfg.Default.SeparatePerHandlerEnabled, lo.ToPtr(false)))
	cfg.Default.MainConfig = defaultConfig(cfg.Default.MainConfig)

	for idx, h := range cfg.Handlers {
		cfg.Handlers[idx].MainConfig = enrich(h.MainConfig, cfg.Default.MainConfig)
	}

	return cfg
}

func enrich(cfg, enrichment MainConfig) MainConfig {
	cfg.Enabled = lo.ToPtr(*overrideIfZero(cfg.Enabled, enrichment.Enabled))
	cfg.MaxAttempts = overrideIfZero(cfg.MaxAttempts, enrichment.MaxAttempts)
	cfg.InitialBackoff = overrideIfZero(cfg.InitialBackoff, enrichment.InitialBackoff)
	cfg.MaxBackoff = overrideIfZero(cfg.MaxBackoff, enrichment.MaxBackoff)
	cfg.BackoffMultiplier = overrideIfZero(cfg.BackoffMultiplier, enrichment.BackoffMultiplier)

	cfg.Throttler.Enabled = lo.ToPtr(*overrideIfZero(cfg.Throttler.Enabled, enrichment.Throttler.Enabled))
	cfg.Throttler.MaxTokens = overrideIfZero(cfg.Throttler.MaxTokens, enrichment.Throttler.MaxTokens)
	cfg.Throttler.TokenRatio = overrideIfZero(cfg.Throttler.TokenRatio, enrichment.Throttler.TokenRatio)

	cfg.RetryableStatusCodes = slices.Clone(overrideIfNilSlice(cfg.RetryableStatusCodes, enrichment.RetryableStatusCodes))
	return cfg
}

func defaultConfig(cfg MainConfig) MainConfig {
	defaultCfg := MainConfig{
		Enabled:           lo.ToPtr(false),
		MaxAttempts:       3,
		InitialBackoff:    50 * time.Millisecond,
		MaxBackoff:        1 * time.Second,
		BackoffMultiplier: 1.5,

		Throttler: ThrottlerConfig{
			Enabled:    lo.ToPtr(false),
			MaxTokens:  10,
			TokenRatio: 0.1,
		},
		RetryableStatusCodes: []string{"Unavailable"},
	}

	return enrich(cfg, defaultCfg)
}
