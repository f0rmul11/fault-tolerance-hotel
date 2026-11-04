package hedge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/samber/lo"
	"gitlab.com/NebulousLabs/fastrand"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	singleDCacheResetPeriod = 500 * time.Millisecond
)

// StrategySingleD (Single-Time/Deterministic) is policy with single additional request reissued after stat-based delay with 100% probability.
type StrategySingleD struct {
	cfg MainConfig

	latencyRingBuf *DurationBuf

	muCounters        sync.Mutex
	countersResetTime time.Time
	primaryReqCount   uint64
	hedgedReqCount    uint64

	muCache        sync.RWMutex
	delayCached    time.Duration
	cacheResetTime time.Time
}

func NewStrategySingleD(cfg MainConfig) *StrategySingleD {
	return &StrategySingleD{
		cfg:               cfg,
		latencyRingBuf:    NewDurationBuf(cfg.WindowSize),
		countersResetTime: time.Now().Add(cfg.ResetPeriod),
		cacheResetTime:    time.Now().Add(singleDCacheResetPeriod),
	}
}

func (s *StrategySingleD) Execute(ctx context.Context, fn RPCFunc) (interface{}, error) {
	respCh := make(chan rpcResponse, 2)

	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	execute := func(latencyTracker func(time.Duration), primary bool) {
		t := time.Now()
		msg, err := fn(childCtx)
		d := time.Since(t)

		role := lo.Ternary(primary, "primary", "hedged")
		slog.Info(fmt.Sprintf("[%s] elapsed: %s", role, d.String()))

		// Отмена измеряет время до остановки, а не до получения ответа.
		// Проверяем результат RPC: успешная попытка могла завершиться до отмены контекста.
		if !errors.Is(err, context.Canceled) && status.Code(err) != codes.Canceled {
			latencyTracker(d)
		}

		select {
		case <-childCtx.Done():
			return
		case respCh <- rpcResponse{msg: msg, err: err}:
		}
	}

	// primary request
	// Сбрасываем истёкшее окно до учёта primary, а не при запуске его hedge.
	s.muCounters.Lock()
	now := time.Now()
	if !now.Before(s.countersResetTime) {
		s.primaryReqCount = 0
		s.hedgedReqCount = 0
		s.countersResetTime = now.Add(s.cfg.ResetPeriod)
	}
	s.primaryReqCount++
	s.muCounters.Unlock()

	go func() {
		slog.Info(fmt.Sprintf("execute primary at '%s'", time.Now().String()))
		execute(s.latencyRingBuf.Append, true)
	}()

	// hedged request
	d, q := s.computeOptimalDelay(s.cfg.PercentileOffset)

	if q == 1 || q > 0 && fastrand.Intn(100) < int(q*100) {
		go func() {
			select {
			case <-childCtx.Done():
				return
			case <-time.After(d):
				if !s.reserveHedge() {
					return
				}

				slog.Info(fmt.Sprintf("execute hedged at '%s' after %s", time.Now().String(), d.String()))
				execute(s.latencyRingBuf.Append, false)
			}
		}()
	}

	select {
	case <-ctx.Done():
		return nil, status.FromContextError(ctx.Err()).Err()
	case resp := <-respCh:
		// если primary слишком быстрый и обгоняет подсчитанный delay для hedged,
		// то он может успеть отдать ответ и мы вызовем тут defer cancel(), что исключит запуск hedged вовсе
		return resp.msg, resp.err
	}
}

// computeOptimalDelay calculates optimal delay `d` and probability `q`
// for request hedged with target percentile and budget.
func (s *StrategySingleD) computeOptimalDelay(offset float64) (d time.Duration, q float64) {
	if s.cfg.HedgeBudget <= 0 {
		return 0, 0
	}

	q = 1

	now := time.Now()

	s.muCache.RLock()
	if now.Before(s.cacheResetTime) && s.delayCached > 0 {
		defer s.muCache.RUnlock()

		return s.delayCached, q
	}
	s.muCache.RUnlock()

	respTimes := s.latencyRingBuf.Copy()
	if len(respTimes) == 0 || s.cfg.Warmup && len(respTimes) < s.cfg.WindowSize {
		return 0, 0
	}

	sort.Slice(respTimes, func(i, j int) bool {
		return respTimes[i] < respTimes[j]
	})

	slog.Info(fmt.Sprintf("ring latency buf: %v", lo.Map(respTimes, func(latency time.Duration, _ int) int64 {
		return latency.Milliseconds()
	})))

	delayPercentile := s.cfg.Percentile - offset

	if delayPercentile < 0 {
		delayPercentile = 0
	}

	d = respTimes[int(float64(len(respTimes)-1)*delayPercentile)]

	/*
		БЕЗ ОФФСЕТА:

		Допустим, у тебя в буфере последние задержки (в миллисекундах):
		latencies = [100, 120, 130, 150, 200, 300, 500]

		И цель — 95-й процентиль:

		index = (len(sorted)-1) * 0.95

		index = (7-1) * 0.95 = 6 * 0.95 ≈ 5.7 → округляем вниз → 5
		sorted[5] = 300ms !!!


		C ОФФСЕТОМ:

		delayPercentile = Percentile - offset

		Percentile = 0.95
		offset = 0.2
		delayPercentile = 0.95 - 0.2 = 0.75

		index = (len(sorted)-1) * 0.75 = 6 * 0.75 = 4.5 → 4
		delay = sorted[4] = 200ms !!!
	*/

	if d == 0 {
		q = 0
	}

	s.muCache.Lock()
	s.delayCached = d
	s.cacheResetTime = now.Add(singleDCacheResetPeriod)
	s.muCache.Unlock()

	return d, q
}

// reserveHedge атомарно проверяет долю с учётом новой попытки и занимает бюджет.
func (s *StrategySingleD) reserveHedge() bool {
	s.muCounters.Lock()
	defer s.muCounters.Unlock()

	now := time.Now()
	if !now.Before(s.countersResetTime) {
		s.primaryReqCount = 0
		s.hedgedReqCount = 0
		s.countersResetTime = now.Add(s.cfg.ResetPeriod)
	}
	nextHedged := s.hedgedReqCount + 1
	total := s.primaryReqCount + nextHedged
	if float64(nextHedged)/float64(total) > s.cfg.HedgeBudget {
		return false
	}

	s.hedgedReqCount = nextHedged
	return true
}
