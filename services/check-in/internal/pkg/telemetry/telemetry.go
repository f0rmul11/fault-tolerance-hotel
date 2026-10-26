// Package telemetry owns all signal providers and their shared OTLP connection.
// All signals share one resource and one owned OTLP connection.
package telemetry

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/gofrs/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	lognoop "go.opentelemetry.io/otel/log/noop"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	"check-in/internal/pkg/telemetry/logs"
	"check-in/internal/pkg/telemetry/metrics"
	"check-in/internal/pkg/telemetry/traces"
)

type Telemetry struct {
	MeterProvider  metric.MeterProvider
	TracerProvider trace.TracerProvider
	LoggerProvider *sdklog.LoggerProvider

	grpcConnection *grpc.ClientConn
	shutdown       []func(context.Context) error
	shutdownOnce   sync.Once
	shutdownError  error
}

// Init is called once before starting listeners, clients and workers. grpc.NewClient
// does not wait for the Collector; the trace exporter queues work asynchronously.
func Init(ctx context.Context, config Config) (*Telemetry, error) {
	config = normalizeConfig(config)
	traces.SetTracerName(config.TracerName)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	otel.SetLoggerProvider(lognoop.NewLoggerProvider())
	otel.SetMeterProvider(metricnoop.NewMeterProvider())
	// SDK/exporter failures must never be emitted into the same OTLP pipeline.
	diagnostics := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		diagnostics.Warn("Ошибка SDK телеметрии", "error", err)
	}))
	slog.SetDefault(logs.NewLogger(config.ServiceName, config.LogLevel, nil))

	if !config.Enabled {
		provider := noop.NewTracerProvider()
		otel.SetTracerProvider(provider)
		return &Telemetry{
			TracerProvider: provider,
			MeterProvider:  otel.GetMeterProvider(),
		}, nil
	}
	res, err := newResource(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("telemetry: resource: %w", err)
	}
	connection, err := newOTLPConnection(config)
	if err != nil {
		return nil, err
	}
	provider, err := traces.NewProvider(ctx, res, connection)
	if err != nil {
		return nil, errors.Join(err, connection.Close())
	}
	var loggerProvider *sdklog.LoggerProvider
	if config.LogsEnabled {
		loggerProvider, err = logs.NewProvider(ctx, res, connection)
		if err != nil {
			return nil, errors.Join(err, provider.Shutdown(ctx), connection.Close())
		}
		otel.SetLoggerProvider(loggerProvider)
	}
	var meterProvider *sdkmetric.MeterProvider
	if config.MetricsEnabled {
		meterProvider, err = metrics.NewProvider(ctx, res, connection, config.MetricsInterval)
		if err != nil {
			var logsError error
			if loggerProvider != nil {
				logsError = loggerProvider.Shutdown(ctx)
			}
			return nil, errors.Join(err, logsError, provider.Shutdown(ctx), connection.Close())
		}
		otel.SetMeterProvider(meterProvider)
	}
	otel.SetTracerProvider(provider)
	slog.SetDefault(logs.NewLogger(config.ServiceName, config.LogLevel, loggerProvider))
	telemetry := &Telemetry{
		TracerProvider: provider,
		MeterProvider:  otel.GetMeterProvider(),
		LoggerProvider: loggerProvider,
		grpcConnection: connection,
	}
	if loggerProvider != nil {
		telemetry.shutdown = append(telemetry.shutdown, loggerProvider.Shutdown)
	}
	if meterProvider != nil {
		telemetry.shutdown = append(telemetry.shutdown, meterProvider.Shutdown)
	}
	telemetry.shutdown = append(telemetry.shutdown, provider.Shutdown)
	return telemetry, nil
}

// Shutdown flushes providers while their shared connection is still available,
// then closes that connection. Repeated calls do not close resources twice.
func (telemetry *Telemetry) Shutdown(ctx context.Context) error {
	if telemetry == nil {
		return nil
	}
	telemetry.shutdownOnce.Do(func() {
		for _, shutdown := range telemetry.shutdown {
			telemetry.shutdownError = errors.Join(telemetry.shutdownError, shutdown(ctx))
		}
		if telemetry.grpcConnection != nil {
			telemetry.shutdownError = errors.Join(telemetry.shutdownError, telemetry.grpcConnection.Close())
		}
	})
	return telemetry.shutdownError
}

func newResource(ctx context.Context, config Config) (*resource.Resource, error) {
	instanceID, err := uuid.NewV4()
	if err != nil {
		return nil, fmt.Errorf("service.instance.id: %w", err)
	}
	attributes := []attribute.KeyValue{
		attribute.String("service.name", config.ServiceName),
		attribute.String("service.namespace", config.ServiceNamespace),
		attribute.String("service.instance.id", instanceID.String()),
	}
	if config.ServiceVersion != "" {
		attributes = append(attributes, attribute.String("service.version", config.ServiceVersion))
	}
	return resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithContainer(),
		resource.WithOS(),
		resource.WithProcessPID(),
		resource.WithProcessExecutableName(),
		resource.WithProcessExecutablePath(),
		resource.WithProcessRuntimeName(),
		resource.WithProcessRuntimeVersion(),
		resource.WithProcessRuntimeDescription(),
		resource.WithAttributes(attributes...),
	)
}

func newOTLPConnection(config Config) (*grpc.ClientConn, error) {
	endpoint := normalizeGRPCEndpoint(config.OTLPEndpoint)
	var transportCredentials credentials.TransportCredentials
	if config.OTLPInsecure {
		transportCredentials = insecure.NewCredentials()
	} else {
		transportCredentials = credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
	}
	connection, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(transportCredentials))
	if err != nil {
		return nil, fmt.Errorf("telemetry: create OTLP connection: %w", err)
	}
	return connection, nil
}
