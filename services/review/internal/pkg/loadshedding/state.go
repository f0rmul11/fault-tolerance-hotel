package loadshedding

import (
	"github.com/platinummonkey/go-concurrency-limits/core"
	"github.com/platinummonkey/go-concurrency-limits/limit"
	"github.com/platinummonkey/go-concurrency-limits/limit/functions"
	"github.com/platinummonkey/go-concurrency-limits/limiter"
	"github.com/platinummonkey/go-concurrency-limits/strategy"
)

const (
	component = "load_shedding"
)

type stateByHandlers struct {
	cfg            Config
	handlersConfig map[string]HandlerConfig
	handlersState  map[string]*internalState
	defaultState   *internalState
}

type internalState struct {
	cfg     MainConfig
	metrics *metricRegistry
	// limitAlg алгоритм, с помощью которого происходит detection нездоровой обстановки
	// и изменение лимита
	limitAlg core.Limit
	// queueLimiter примитив, который отвечает на вопрос "а можно ли сделать запрос? Или тут меня выгонят?"
	queueLimiter core.Limiter
}

func newInternalState(config MainConfig, handler string) (*internalState, error) {
	metrics := newMetricRegistry(handler, config.Vegas.InitialMaxInflightLimit)

	// создаем noop logger
	logger := limit.Logger(limit.NoopLimitLogger{})

	// выбираем самую простую стратегию, которая
	// управляет общим лимитом без ухищрений в виде партиций, типа нагрузки и т.п.
	simpleStrategy := strategy.NewSimpleStrategy(config.Vegas.InitialMaxInflightLimit)

	// подготавливаем параметры для алгоритма лимитирования
	defaultLogFunc := functions.Log10RootFunction(0)
	// расчет для alpha коэффициента
	alphaFunc := func(limit int) int {
		return config.Vegas.AlphaFuncCoefficient * defaultLogFunc(limit)
	}
	// расчет для beta коэффициента
	betaFunc := func(limit int) int {
		return config.Vegas.BetaFuncCoefficient * defaultLogFunc(limit)
	}

	// выбираем изученный Vegas алгоритм
	algorithm := limit.NewVegasLimitWithRegistry(
		component,
		config.Vegas.InitialMaxInflightLimit,  // передаем стартовое значение лимита (минуем фазу slow start)
		nil,                                   // говорим, что изначально нет такого значения и иди сам посчитай
		config.Vegas.MaxInflightLimit,         // говорим, что выше этого лимита нельзя подниматься автоматикой
		config.Vegas.Smoothing,                // задаем сглаживание, чтобы при изменении лимита не было резкости
		alphaFunc,                             // наша альфа
		betaFunc,                              // наша бета
		nil,                                   // не задаем каких-то условий, касаемо threshold'а. И пусть библиотека сама учитывает его, исходя из лимита
		nil,                                   // точно так же с функцией роста лимита
		nil,                                   // точно так же с функцией падения лимита
		config.Vegas.RTTNoLoadProbeMultiplier, // служит множителем, который помогает регулировать частотность взятия проб RTTNoLoad
		logger,
		metrics, // штатный MetricRegistry библиотеки
	)

	algorithm.NotifyOnChange(metrics.limitChanged)
	observed := &observedVegas{
		VegasLimit: algorithm,
		metrics:    metrics,
	}

	// создаем обертку над алгоритмом и стратегией в условиях ограничения (окно временное), ибо считать за все время нельзя
	defaultLimiter, err := limiter.NewDefaultLimiter(
		observed, // наблюдение не меняет расчёты алгоритма
		config.Limiter.MinWindowTime.Nanoseconds(),   // минимальный размер окна для перерасчета (время)
		config.Limiter.MaxWindowTime.Nanoseconds(),   // максимальный размер окна для перерасчета (время)
		config.Limiter.MinRTTThreshold.Nanoseconds(), // минимальный latency, ниже которого не учитываем значения
		config.Limiter.WindowSize,                    // размер окна в запросах!
		simpleStrategy,                               // стратегия
		logger,
		metrics,
	)
	if err != nil {
		return nil, err
	}

	lifoQueueLimiter := limiter.NewQueueBlockingLimiterFromConfig(defaultLimiter, limiter.QueueLimiterConfig{
		// Используем LIFO (last-in first-out) для того, чтобы минимально жертвовать задержками для свежих запросов
		// в угоду доступности. Свежие запросы будут обработаны первые, если для них будет "место" (лимит позволит).
		// В свою очередь более ранние запросы будут торчать в очереди, пока до них не дойдет черед
		Ordering:            limiter.OrderingLIFO,
		MaxBacklogSize:      config.Limiter.MaxBacklogSize,        // задаем предел очереди
		MaxBacklogTimeout:   config.Limiter.MaxBacklogWaitingTime, // задаем предел времени ожидания
		BacklogEvictDoneCtx: true,
		MetricRegistry:      metrics,
	})

	metrics.suppliers["inflight"] = core.NewIntMetricSupplierWrapper(simpleStrategy.GetBusyCount)
	metrics.suppliers["vegas_min_rtt_seconds"] = core.NewFloat64MetricSupplierWrapper(func() float64 {
		return float64(algorithm.RTTNoLoad()) / 1e9
	})
	metrics.suppliers["vegas_alpha"] = core.NewIntMetricSupplierWrapper(func() int {
		return alphaFunc(algorithm.EstimatedLimit())
	})
	metrics.suppliers["vegas_beta"] = core.NewIntMetricSupplierWrapper(func() int {
		return betaFunc(algorithm.EstimatedLimit())
	})
	liveMetrics.states.Store(handler, metrics)

	return &internalState{
		cfg:          config,
		metrics:      metrics,
		limitAlg:     observed,
		queueLimiter: lifoQueueLimiter,
	}, nil
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

	if !*s.cfg.Default.SeparatePerHandlerEnabled && s.defaultState != nil {
		return s.defaultState, true
	}

	return nil, true
}

func (s stateByHandlers) isHandlerEnabled(method string) bool {
	handlerConfig, ok := s.handlersConfig[method]
	if ok {
		return handlerConfig.Enabled
	}

	return s.cfg.Default.Enabled
}
