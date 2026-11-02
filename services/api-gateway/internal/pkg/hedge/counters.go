package hedge

import (
	"sync"
	"time"
)

type DurationBuf struct {
	mu      sync.RWMutex
	data    []time.Duration
	current int
	count   int // Число заполненных ячеек, не больше размера буфера.
}

func NewDurationBuf(size int) *DurationBuf {
	return &DurationBuf{
		data: make([]time.Duration, size),
	}
}

func (s *DurationBuf) Append(v time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[s.current] = v
	if s.count < len(s.data) {
		s.count++
	}
	s.current++

	if s.current >= len(s.data) {
		s.current = 0
	}
}

func (s *DurationBuf) Copy() []time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stat := make([]time.Duration, s.count)

	copy(stat, s.data[:s.count])

	return stat
}
