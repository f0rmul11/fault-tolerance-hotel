package circuit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/sony/gobreaker/v2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type cbStateByHandlers struct {
	cfg            Config
	handlersConfig map[string]HandlerConfig
	handlersState  map[string]*internalState
	defaultState   *internalState
}

type internalState struct {
	cfg     MainConfig
	breaker *gobreaker.CircuitBreaker[any]
}

func newInternalState(config MainConfig, name string) *internalState {
	settings := gobreaker.Settings{
		Name:         name,
		MaxRequests:  config.MaxRequests,
		Interval:     config.Interval,
		Timeout:      config.OpenTimeout,
		BucketPeriod: config.BucketPeriod,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			// Срабатываем, если количество ошибок превышает порог
			// Тут смотрим как на последовательные ошибки, так и на общий процент в окне
			// Canceled and still pending requests do not prove dependency health.
			observations := counts.TotalSuccesses + counts.TotalFailures
			shouldOpen := observations > 10 && (counts.ConsecutiveFailures >= config.ThresholdConsecutive ||
				(counts.TotalFailures*100/observations) > config.ThresholdPercentage)

			slog.Info(fmt.Sprintf("circuit should open: %v", shouldOpen),
				"total", counts.Requests,
				"total_successes", counts.TotalSuccesses,
				"total_failures", counts.TotalFailures,
				"consecutive_successes", counts.ConsecutiveSuccesses,
				"consecutive_failures", counts.ConsecutiveFailures,
			)

			return shouldOpen
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			slog.Info(fmt.Sprintf("circuit breaker '%s' changed state: %s → %s", name, from, to))
		},
		IsSuccessful: func(err error) bool {
			return !triggerOnError(err, config.failureCodeSet())
		},
		IsExcluded: func(err error) bool {
			var excluded *excludedError
			return errors.As(err, &excluded) || errors.Is(err, context.Canceled) || status.Code(err) == codes.Canceled
		},
	}

	return &internalState{
		breaker: gobreaker.NewCircuitBreaker[any](settings),
		cfg:     config,
	}
}

func (s cbStateByHandlers) get(method string) (_ *internalState, enabled bool) {
	st, ok := s.handlersState[method]
	if ok {
		return st, true
	}

	isEnabled := s.isHandlerEnabled(method)
	if !isEnabled {
		return nil, false
	}

	if !*s.cfg.Default.SeparatePerHandlerEnabled && s.defaultState != nil {
		return s.defaultState, true
	}

	return nil, true
}

func (s cbStateByHandlers) isHandlerEnabled(method string) bool {
	handlerConfig, ok := s.handlersConfig[method]
	if ok {
		return handlerConfig.Enabled
	}

	return s.cfg.Default.Enabled
}
