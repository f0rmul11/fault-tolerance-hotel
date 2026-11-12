package loadshedding

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
	// Enabled включена ли feature
	Enabled bool `yaml:"enabled"`
	// Limiter конфигурация limiter'а
	Limiter LimiterConfig `yaml:"limiter"`
	// Vegas алгоритм
	Vegas VegasAlgorithmConfig `yaml:"vegas,omitempty"`
}

type LimiterConfig struct {
	// WindowSize размер окна по количеству запросов для пересчета лимита
	WindowSize int `yaml:"window_size"`
	// MinWindowTime минимальный размер окна по времени для пересчета лимита
	MinWindowTime time.Duration `yaml:"min_window_time"`
	// MaxWindowTime максимальный размер окна по времени для пересчета лимита
	MaxWindowTime time.Duration `yaml:"max_window_time"`
	// MinRTTThreshold - порог RTT, все значения ниже этого никак не учитываются в алгоритме
	MinRTTThreshold time.Duration `yaml:"min_rtt_threshold"`

	// MaxBacklogSize максимальный размер очереди
	MaxBacklogSize int `yaml:"max_backlog_size"`
	// MaxInQueueWaitingTime максимальное время ожидания запроса в очереди, после истечения по запросу возвращается ошибка (и пишется dropped метрика)
	MaxBacklogWaitingTime time.Duration `yaml:"max_backlog_wait_time"`
}

type VegasAlgorithmConfig struct {
	// InitialMaxInflightLimit стартовое значение max inflight до того как будет достаточно
	// запросов, чтобы начать рассчитывать лимит
	InitialMaxInflightLimit int `yaml:"initial_max_inflight_limit"`
	// MaxMaxInflightLimit максимальный лимит max inflight, до которого можно повысить
	MaxInflightLimit int `yaml:"max_inflight_limit"`
	// Smoothing отвечает за коэффициент резкости смены нового лимита.
	// Выставляется от 0 до 1.0, где 1.0 - резко пересчитывать с учетом новых данных
	Smoothing float64 `yaml:"smoothing"`
	// RTTNoLoadProbeMultiplier отвечает за то как часто берётся random probe latency как rttNoLoad
	RTTNoLoadProbeMultiplier int `yaml:"rtt_no_load_probe_multiplier"`
	// AlphaFuncCoefficient Alpha коэффициент в формуле TCP Vegas
	AlphaFuncCoefficient int `yaml:"alpha_func_coefficient"`
	// BetaFuncCoefficient Beta коэффициент в формуле TCP Vegas
	BetaFuncCoefficient int `yaml:"beta_func_coefficient"`
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
	cfg.Enabled = overrideIfZero(cfg.Enabled, enrichment.Enabled)

	cfg.Limiter.WindowSize = overrideIfZero(cfg.Limiter.WindowSize, enrichment.Limiter.WindowSize)
	cfg.Limiter.MinWindowTime = overrideIfZero(cfg.Limiter.MinWindowTime, enrichment.Limiter.MinWindowTime)
	cfg.Limiter.MaxWindowTime = overrideIfZero(cfg.Limiter.MaxWindowTime, enrichment.Limiter.MaxWindowTime)
	cfg.Limiter.MinRTTThreshold = overrideIfZero(cfg.Limiter.MinRTTThreshold, enrichment.Limiter.MinRTTThreshold)
	cfg.Limiter.MaxBacklogSize = overrideIfZero(cfg.Limiter.MaxBacklogSize, enrichment.Limiter.MaxBacklogSize)
	cfg.Limiter.MaxBacklogWaitingTime = overrideIfZero(cfg.Limiter.MaxBacklogWaitingTime, enrichment.Limiter.MaxBacklogWaitingTime)

	cfg.Vegas.InitialMaxInflightLimit = overrideIfZero(cfg.Vegas.InitialMaxInflightLimit, enrichment.Vegas.InitialMaxInflightLimit)
	cfg.Vegas.MaxInflightLimit = overrideIfZero(cfg.Vegas.MaxInflightLimit, enrichment.Vegas.MaxInflightLimit)
	cfg.Vegas.RTTNoLoadProbeMultiplier = overrideIfZero(cfg.Vegas.RTTNoLoadProbeMultiplier, enrichment.Vegas.RTTNoLoadProbeMultiplier)
	cfg.Vegas.Smoothing = overrideIfZero(cfg.Vegas.Smoothing, enrichment.Vegas.Smoothing)
	cfg.Vegas.AlphaFuncCoefficient = overrideIfZero(cfg.Vegas.AlphaFuncCoefficient, enrichment.Vegas.BetaFuncCoefficient)
	cfg.Vegas.BetaFuncCoefficient = overrideIfZero(cfg.Vegas.BetaFuncCoefficient, enrichment.Vegas.BetaFuncCoefficient)

	return cfg
}

func defaultConfig(cfg MainConfig) MainConfig {
	defaultCfg := MainConfig{
		Enabled: false,
		Limiter: LimiterConfig{
			WindowSize:            100,
			MinWindowTime:         time.Second,
			MaxWindowTime:         2 * time.Second,
			MinRTTThreshold:       700 * time.Microsecond,
			MaxBacklogSize:        1000,
			MaxBacklogWaitingTime: time.Second * 5,
		},
		Vegas: VegasAlgorithmConfig{
			InitialMaxInflightLimit:  50,
			MaxInflightLimit:         10000,
			Smoothing:                0.3,
			RTTNoLoadProbeMultiplier: 25,
			AlphaFuncCoefficient:     3,
			BetaFuncCoefficient:      6,
		},
	}

	return enrich(cfg, defaultCfg)
}
