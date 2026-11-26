package negative

import (
	"time"

	"hotels/config"
	app "hotels/internal/pkg/hotelserror"
)

// Payload хранит публичные поля ошибки каталога.
type Payload struct {
	Code           app.Code `json:"code"`
	Message        string   `json:"message"`
	Classification string   `json:"classification,omitempty"`
}

// Classify выбирает TTL категории. Нулевой TTL отключает запись.
func Classify(err error, policy config.NegativeCache) (*app.Error, time.Duration, bool) {
	cause, ok := app.As(err)
	if !ok {
		return nil, 0, false
	}

	var ttl time.Duration
	switch cause.Code() {
	case app.NotFound:
		ttl = policy.NotFoundTTL
	case app.Unavailable:
		ttl = policy.UnavailableTTL
	case app.ResourceExhausted:
		ttl = policy.ResourceExhaustedTTL
	}
	if ttl <= 0 {
		return nil, 0, false
	}
	return cause, ttl, true
}
