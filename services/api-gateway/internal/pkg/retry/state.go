package retry

import (
	"api-gateway/internal/pkg/retry/throttler"

	"google.golang.org/grpc/codes"
)

type stateByHandlers struct {
	cfg             Config
	handlersConfig  map[string]HandlerConfig
	handlersState   map[string]*internalState
	sharedThrottler *throttler.Throttler
}

type internalState struct {
	cfg            MainConfig
	retryableCodes map[codes.Code]struct{}

	throttler *throttler.Throttler
}

func newInternalState(config MainConfig) *internalState {
	var th *throttler.Throttler
	if *config.Throttler.Enabled {
		th = throttler.NewThrottler(config.Throttler.MaxTokens, config.Throttler.TokenRatio)
	}

	return &internalState{
		cfg:            config,
		retryableCodes: config.retryableCodesSet(),
		throttler:      th,
	}
}

func (s stateByHandlers) get(method string) (_ *internalState, enabled bool) {
	st, ok := s.handlersState[method]
	if ok {
		return st, true
	}

	isEnabled := s.isHandlerEnabled(method)
	if !isEnabled {
		return nil, false
	}

	return nil, true
}

func (s stateByHandlers) isHandlerEnabled(method string) bool {
	handlerConfig, ok := s.handlersConfig[method]
	if ok {
		return *handlerConfig.Enabled
	}

	return *s.cfg.Default.Enabled
}
