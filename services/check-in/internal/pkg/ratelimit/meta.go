package ratelimit

import (
	"context"
	"math"
	"strconv"
	"time"

	"golang.org/x/time/rate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// Снимок квоты носит рекомендательный характер: другие RPC могут расходовать
// токены одновременно. TokensAt не резервирует и не расходует токены.
func setHeaders(ctx context.Context, limiter *rate.Limiter, rejected bool) {
	now := time.Now()
	tokens := limiter.TokensAt(now)
	limit := float64(limiter.Limit())
	headers := metadata.Pairs(
		"x-ratelimit-limit", strconv.FormatFloat(limit, 'f', -1, 64),
		"x-ratelimit-burst", strconv.Itoa(limiter.Burst()),
		"x-ratelimit-remaining", strconv.FormatFloat(math.Floor(math.Max(0, tokens)), 'f', 0, 64),
	)
	if limit > 0 {
		// Reset — Unix timestamp полного наполнения при отсутствии новых запросов.
		// При нулевой скорости конечного момента восстановления нет.
		reset := math.Ceil(float64(now.Unix()) + float64(now.Nanosecond())/1e9 + math.Max(0, float64(limiter.Burst())-tokens)/limit)
		if reset < float64(math.MaxInt64) {
			headers.Set("x-ratelimit-reset", strconv.FormatInt(int64(reset), 10))
		}
		if rejected {
			delay := math.Max(1, math.Ceil(math.Max(0, 1-tokens)/limit))
			if delay < float64(math.MaxInt64) {
				headers.Set("retry-after", strconv.FormatInt(int64(delay), 10))
			}
		}
	}
	_ = grpc.SetHeader(ctx, headers)
}
