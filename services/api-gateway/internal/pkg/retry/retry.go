package retry

import (
	"maps"
	"sync"
	"sync/atomic"

	"api-gateway/internal/pkg/retry/throttler"
)

type Retry struct {
	state *atomic.Pointer[stateByHandlers]
	lock  sync.Mutex
}

// NewRetry создаёт долгоживущий retry для одного downstream-соединения.
func NewRetry(config Config) *Retry {
	stateAtomic := &atomic.Pointer[stateByHandlers]{}

	enriched := withDefaults(config)
	state := stateByHandlers{
		cfg:            enriched,
		handlersConfig: enriched.handlersConfig(),
		handlersState:  make(map[string]*internalState),
	}

	if !*enriched.Default.SeparatePerHandlerEnabled {
		state.sharedThrottler = throttler.NewThrottler(enriched.Default.Throttler.MaxTokens, enriched.Default.Throttler.TokenRatio)
	}
	stateAtomic.Store(&state)

	return &Retry{
		state: stateAtomic,
	}
}

func (r *Retry) getByMethod(method string) (*internalState, bool) {
	// fast path
	snapshot := r.state.Load()

	state, enabled := snapshot.get(method)
	if enabled && state != nil {
		return state, true
	} else if !enabled {
		return nil, false
	}

	// slow path
	r.lock.Lock()
	defer r.lock.Unlock()

	snapshot = r.state.Load()

	state, enabled = snapshot.get(method)
	if enabled && state != nil {
		return state, true
	} else if !enabled {
		return nil, false
	}

	// Определяем конфигурацию для метода
	var mainConfig MainConfig
	if handlerConfig, ok := snapshot.handlersConfig[method]; ok {
		mainConfig = handlerConfig.MainConfig
	} else {
		mainConfig = snapshot.cfg.Default.MainConfig
	}

	updated := *snapshot
	state = newInternalState(mainConfig)
	if !*snapshot.cfg.Default.SeparatePerHandlerEnabled && *mainConfig.Throttler.Enabled {
		state.throttler = snapshot.sharedThrottler
	}
	// Method policy всегда отдельная; общим может быть только throttler.
	handlersState := maps.Clone(snapshot.handlersState)
	handlersState[method] = state
	updated.handlersState = handlersState

	r.state.Store(&updated)

	return state, true
}
