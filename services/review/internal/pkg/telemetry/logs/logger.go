package logs

import (
	"context"
	"log/slog"
	"os"
	"strings"

	slogmulti "github.com/samber/slog-multi"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/trace"
)

// NewLogger preserves stdout and adds OTLP when a provider is available.
// Only OTLP is ingested by Collector, so stdout does not create duplicate records.
func NewLogger(serviceName, logLevel string, provider *sdklog.LoggerProvider) *slog.Logger {
	level := parseLevel(logLevel)
	var handler slog.Handler = &contextHandler{next: slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     level,
	})}
	if provider != nil {
		handler = slogmulti.Fanout(handler, otelslog.NewHandler(serviceName,
			otelslog.WithLoggerProvider(provider),
			otelslog.WithSource(true),
		))
	}
	// Apply the same threshold to stdout and OTLP; the bridge has no slog level option.
	handler = slogmulti.NewEnabledInlineMiddleware(func(ctx context.Context, recordLevel slog.Level, next func(context.Context, slog.Level) bool) bool {
		return recordLevel >= level && next(ctx, recordLevel)
	})(handler)
	return slog.New(handler)
}

func parseLevel(value string) slog.Level {
	var level slog.Level
	if err := level.UnmarshalText([]byte(strings.ToLower(strings.TrimSpace(value)))); err == nil {
		return level
	}
	return slog.LevelInfo
}

// contextHandler adds correlation to stdout. OTLP uses the native LogRecord IDs.
type contextHandler struct {
	next slog.Handler
}

func (h *contextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *contextHandler) Handle(ctx context.Context, record slog.Record) error {
	sc := trace.SpanContextFromContext(ctx)
	if sc.IsValid() {
		record = record.Clone()
		record.AddAttrs(slog.String("trace_id", sc.TraceID().String()), slog.String("span_id", sc.SpanID().String()))
	}
	return h.next.Handle(ctx, record)
}

func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{next: h.next.WithAttrs(attrs)}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{next: h.next.WithGroup(name)}
}
