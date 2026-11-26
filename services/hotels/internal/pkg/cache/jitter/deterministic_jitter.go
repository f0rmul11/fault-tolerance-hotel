package jitter

import (
	"hash/fnv"
	"math/rand"
	"time"
)

// DeterministicJitter - одинаковый jitter для одного ключа
// Удобно для отладки и предсказуемого поведения
func DeterministicJitter(key string, baseTTL time.Duration, jitterPercent float64) time.Duration {
	// Создаем детерминированный источник rand на основе ключа
	h := fnv.New64a()
	h.Write([]byte(key))
	seed := int64(h.Sum64())

	r := rand.New(rand.NewSource(seed))

	jitterRange := float64(baseTTL) * jitterPercent
	jitter := time.Duration(r.Float64() * jitterRange)

	return baseTTL + jitter
}
