package loadshedding

import (
	"maps"
	"sync"
	"sync/atomic"
)

type LoadShedding struct {
	state *atomic.Pointer[stateByHandlers]
	lock  sync.Mutex
}

func NewLoadShedding(config Config) *LoadShedding {
	stateAtomic := &atomic.Pointer[stateByHandlers]{}

	enriched := withDefaults(config)
	state := stateByHandlers{
		cfg:            enriched,
		handlersConfig: enriched.handlersConfig(),
		handlersState:  make(map[string]*internalState),
	}

	stateAtomic.Store(&state)

	return &LoadShedding{
		state: stateAtomic,
	}
}

func (ls *LoadShedding) getByMethod(method string) (*internalState, bool) {
	// fast path
	loadedState := ls.state.Load()

	state, enabled := loadedState.get(method)
	if enabled && state != nil {
		return state, true
	} else if !enabled {
		return nil, false
	}

	// slow path
	ls.lock.Lock()
	defer ls.lock.Unlock()

	loadedState = ls.state.Load()

	state, enabled = loadedState.get(method)
	if enabled && state != nil {
		return state, true
	} else if !enabled {
		return nil, false
	}

	// Определяем конфигурацию для метода
	var mainConfig MainConfig
	if handlerConfig, ok := loadedState.handlersConfig[method]; ok {
		mainConfig = handlerConfig.MainConfig
	} else {
		mainConfig = loadedState.cfg.Default.MainConfig
	}

	var (
		err error
	)
	newState := *loadedState
	if !*loadedState.cfg.Default.SeparatePerHandlerEnabled {
		state, err = newInternalState(loadedState.cfg.Default.MainConfig, "__shared__")
		if err != nil {
			return nil, false
		}
		newState.defaultState = state
	} else {
		state, err = newInternalState(mainConfig, method)
		if err != nil {
			return nil, false
		}

		// копируем, чтобы не было конкурентных read + write
		handlersState := maps.Clone(loadedState.handlersState)
		handlersState[method] = state
		newState.handlersState = handlersState
	}

	ls.state.Store(&newState)

	return state, true
}
