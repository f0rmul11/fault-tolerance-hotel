package ratelimit

import (
	"context"
	"slices"
	"sync/atomic"
	"time"

	"check-in/internal/pkg/grpc/clientname"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Limiter struct {
	state *atomic.Pointer[state]
}

// NewLimiter сохраняет API исходного пакета и создаёт bucket для каждого правила.
func NewLimiter(config Config) *Limiter {
	st := &state{
		rules: make([]*internalState, 0, len(config.Rules)),
	}
	for i, rule := range config.Rules {
		st.rules = append(st.rules, newInternalState(rule, i))
		st.cfg.Rules = append(st.cfg.Rules, st.rules[i].cfg)
	}
	pointer := &atomic.Pointer[state]{}
	pointer.Store(st)
	return &Limiter{state: pointer}
}

func (l *Limiter) match(st *state, client, method string) *internalState {
	for _, rule := range st.rules {
		clientMatch := len(rule.cfg.Clients) == 0 || slices.Contains(rule.cfg.Clients, client)
		handlerMatch := len(rule.cfg.Handlers) == 0 || slices.Contains(rule.cfg.Handlers, method)
		if clientMatch && handlerMatch {
			return rule
		}
	}
	return nil
}

func (l *Limiter) doIfAllowed(ctx context.Context, method string, fn func() error) error {
	rule := l.match(l.state.Load(), clientname.FromContext(ctx), method)
	if rule == nil {
		return fn()
	}
	if err := ctx.Err(); err != nil {
		rule.metrics.canceled.Inc()

		setHeaders(ctx, rule.limiter, false)
		return status.FromContextError(err).Err()
	}

	allowed := false
	switch {
	case rule.cfg.Limit > 0 && rule.cfg.Timeout > 0:
		started := time.Now()
		waitCtx, cancel := context.WithTimeout(ctx, rule.cfg.Timeout)

		err := rule.limiter.Wait(waitCtx)
		cancel()

		rule.metrics.wait.Observe(time.Since(started).Seconds())
		allowed = err == nil
	case rule.cfg.Limit > 0:
		allowed = rule.limiter.Allow()
	}

	// Истечение бюджета исходного RPC не является превышением квоты.
	if err := ctx.Err(); err != nil {
		rule.metrics.canceled.Inc()
		setHeaders(ctx, rule.limiter, false)

		return status.FromContextError(err).Err()
	}

	setHeaders(ctx, rule.limiter, !allowed)

	if !allowed {
		rule.metrics.rejected.Inc()
		return status.Error(codes.ResourceExhausted, "Превышен лимит запросов к методу сервиса")
	}
	rule.metrics.allowed.Inc()
	return fn()
}
