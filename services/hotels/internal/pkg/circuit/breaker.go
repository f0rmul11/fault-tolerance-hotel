package circuit

import (
	"maps"
	"sync"
	"sync/atomic"
)

// Breaker представляет собой реализацию circuit breaker
type Breaker struct {
	state *atomic.Pointer[cbStateByHandlers]
	lock  sync.Mutex
}

// NewCircuitBreaker создает новый экземпляр CB
func NewCircuitBreaker(config Config) *Breaker {
	stateAtomic := &atomic.Pointer[cbStateByHandlers]{}

	enriched := withDefaults(config)
	state := cbStateByHandlers{
		cfg:            enriched,
		handlersConfig: enriched.handlersConfig(),
		handlersState:  make(map[string]*internalState),
	}

	stateAtomic.Store(&state)

	return &Breaker{
		state: stateAtomic,
	}
}

func (b *Breaker) getBreakerByMethod(method string) (*internalState, bool) {
	// fast path
	cb := b.state.Load()

	state, enabled := cb.get(method)
	if enabled && state != nil {
		return state, true
	} else if !enabled {
		return nil, false
	}

	// slow path
	b.lock.Lock()
	defer b.lock.Unlock()

	cb = b.state.Load()

	state, enabled = cb.get(method)
	if enabled && state != nil {
		return state, true
	} else if !enabled {
		return nil, false
	}

	// Определяем конфигурацию для метода
	var mainConfig MainConfig
	if handlerConfig, ok := cb.handlersConfig[method]; ok {
		mainConfig = handlerConfig.MainConfig
	} else {
		mainConfig = cb.cfg.Default.MainConfig
	}

	newCB := *cb
	if !*cb.cfg.Default.SeparatePerHandlerEnabled {
		state = newInternalState(cb.cfg.Default.MainConfig, method)
		newCB.defaultState = state
	} else {
		state = newInternalState(mainConfig, method)

		// копируем, чтобы не было конкурентных read + write
		handlersState := maps.Clone(cb.handlersState)
		handlersState[method] = state
		newCB.handlersState = handlersState
	}

	b.state.Store(&newCB)

	return state, true
}
