package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/grpc/codes"
)

// One observation per completed incoming request. Resource labels (including
// service_name) are attached by the existing Prometheus -> OTLP bridge.
var serverRequests = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "hotelflow_server_requests_total",
	Help: "Completed incoming requests by transport, route, response category and wire status.",
}, []string{"protocol", "handler", "code", "status"})

// Duration covers server processing, not delivery acknowledgement by the client.
var serverDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name: "hotelflow_server_request_duration_seconds",
	Help: "Server request duration from transport arrival to server completion, in seconds.",
	Buckets: []float64{
		0.001, 0.005, 0.01, 0.025, 0.05,
		0.1, 0.105, 0.11, 0.125, 0.15, 0.2, 0.25, 0.35, 0.5, 0.75,
		1, 1.5, 2, 2.5, 3, 3.5, 4, 4.5, 5, 5.1, 5.25, 5.5, 6, 8, 10, 30,
	},
}, []string{"protocol", "handler", "code"})

func grpcCategory(code codes.Code) string {
	switch code {
	case codes.OK:
		return "ok"
	case codes.Canceled:
		return "canceled"
	case codes.NotFound:
		return "not_found"
	case codes.InvalidArgument, codes.AlreadyExists, codes.PermissionDenied,
		codes.Unauthenticated, codes.FailedPrecondition, codes.Aborted, codes.OutOfRange:
		return "client_error"
	default:
		return "error"
	}
}

func httpCategory(code int) string {
	switch {
	case code == 499:
		return "canceled"
	case code == 404:
		return "not_found"
	case code >= 500:
		return "error"
	case code >= 400:
		return "client_error"
	default:
		return "ok"
	}
}
