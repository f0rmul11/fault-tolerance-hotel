package ratelimit

import (
	"math"
	"slices"

	"golang.org/x/time/rate"
)

type state struct {
	cfg   Config
	rules []*internalState
}

type internalState struct {
	cfg     MainConfig
	limiter *rate.Limiter
	metrics *ruleMetrics
}

func newInternalState(config MainConfig, index int) *internalState {
	config.Clients = slices.Clone(config.Clients)
	config.Handlers = slices.Clone(config.Handlers)
	burst := config.Burst
	if burst == 0 {
		burst = int(math.Ceil(config.Limit))
	}
	// rate.NewLimiter(0, burst) иначе разрешает начальный burst без пополнения.
	if config.Limit == 0 {
		burst = 0
	}
	return &internalState{
		cfg:     config,
		limiter: rate.NewLimiter(rate.Limit(config.Limit), burst),
		metrics: newRuleMetrics(index),
	}
}
