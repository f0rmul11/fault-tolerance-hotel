package hedge

import (
	"context"
	"maps"
	"sync"
	"sync/atomic"
)

type RPCFunc func(ctx context.Context) (msg interface{}, err error)

type rpcResponse struct {
	msg interface{}
	err error
}

type Strategy interface {
	Execute(ctx context.Context, fn RPCFunc) (interface{}, error)
}

// Hedger ...
type Hedger struct {
	state *atomic.Pointer[stateByHandlers]
	lock  sync.Mutex
}

func NewHedger(config Config) *Hedger {
	stateAtomic := &atomic.Pointer[stateByHandlers]{}

	enriched := withDefaults(config)
	state := stateByHandlers{
		cfg:            enriched,
		handlersConfig: enriched.handlersConfig(),
		handlersState:  make(map[string]*internalState),
	}

	stateAtomic.Store(&state)

	return &Hedger{
		state: stateAtomic,
	}
}

func (b *Hedger) getByMethod(method string) (*internalState, bool) {
	// fast path
	hedger := b.state.Load()

	state, enabled := hedger.get(method)
	if enabled && state != nil {
		return state, true
	} else if !enabled {
		return nil, false
	}

	// slow path
	b.lock.Lock()
	defer b.lock.Unlock()

	hedger = b.state.Load()

	state, enabled = hedger.get(method)
	if enabled && state != nil {
		return state, true
	} else if !enabled {
		return nil, false
	}

	// Определяем конфигурацию для метода
	var mainConfig MainConfig
	if handlerConfig, ok := hedger.handlersConfig[method]; ok {
		mainConfig = handlerConfig.MainConfig
	} else {
		mainConfig = hedger.cfg.Default.MainConfig
	}

	newHedger := *hedger
	if !*hedger.cfg.Default.SeparatePerHandlerEnabled {
		state = newInternalState(hedger.cfg.Default.MainConfig)
		newHedger.defaultState = state
	} else {
		state = newInternalState(mainConfig)

		// копируем, чтобы не было конкурентных read + write
		handlersState := maps.Clone(hedger.handlersState)
		handlersState[method] = state
		newHedger.handlersState = handlersState
	}

	b.state.Store(&newHedger)

	return state, true
}
