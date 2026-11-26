package jitter

import (
	"math/rand/v2"
	"time"
)

// UniformPositiveJitter - только увеличиваем TTL
// Самый безопасный и распространенный вариант
// Распределение — равномерное, то есть любое значение в этом диапазоне имеет одинаковую вероятность выпадения.
func UniformPositiveJitter(baseTTL time.Duration, jitterPercent float64) time.Duration {
	jitterRange := float64(baseTTL) * jitterPercent
	jitter := time.Duration(rand.Float64() * jitterRange)
	return baseTTL + jitter
}
