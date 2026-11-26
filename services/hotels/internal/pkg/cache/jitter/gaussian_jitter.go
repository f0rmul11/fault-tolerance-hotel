package jitter

import (
	"math"
	"math/rand/v2"
	"time"
)

// GaussianPositiveJitter - большинство значений близко к baseTTL
// Редкие большие отклонения
func GaussianPositiveJitter(baseTTL time.Duration, jitterPercent float64) time.Duration {
	// отклонение от baseTTL не более чем на jitterPercent
	jitterRange := float64(baseTTL) * jitterPercent

	// Генерируем нормальное распределение
	// 68% значений в пределах 1σ, 95% в пределах 2σ, 99.7% в пределах 3σ

	// Почему делим на 3?
	//
	// В нормальном распределении 99.7% значений лежат в пределах ±3σ
	// Мы хотим, чтобы 99.7% значений попадало в диапазон [0, jitterRange]
	// Поэтому устанавливаем: 3σ = jitterRange ⇒ σ = jitterRange / 3
	sigma := jitterRange / 3

	jitter := math.Abs(rand.NormFloat64()) * sigma

	// Ограничиваем максимальное отклонение
	if jitter > jitterRange {
		jitter = jitterRange
	}

	return baseTTL + time.Duration(jitter)
}
