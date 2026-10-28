package internal

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"notification/config"
	"notification/internal/application/usecase"
	"notification/internal/infrastructure/gateway"
	"notification/internal/infrastructure/handlers/booking_activated"
	"notification/internal/infrastructure/handlers/booking_canceled"
	"notification/internal/infrastructure/handlers/booking_confirmed"
	"notification/internal/infrastructure/messagebus"
	event_router "notification/internal/pkg/event-router"
	"notification/internal/pkg/grpc/intercept"
	"notification/internal/pkg/healthcheck"

	chimw "github.com/go-chi/chi/v5/middleware"

	"notification/internal/app/echo/v1"

	"notification/internal/pkg/closer"
	"notification/internal/pkg/grpcdesc"
	"notification/internal/pkg/telemetry"
	"notification/internal/pkg/telemetry/metrics"
	"notification/internal/pkg/telemetry/traces"

	echoV1 "notification/internal/pkg/pb/notification/echo/v1"

	"github.com/go-chi/chi/v5"

	"github.com/not-for-prod/clay/server"
	"github.com/not-for-prod/clay/transport"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

func (a *App) initControllers(_ context.Context) error {
	a.controllers = []transport.ServiceDesc{
		grpcdesc.New(func(server *grpc.Server) {
			echoV1.RegisterEchoServiceServer(server, echo.NewEchoService())
		}),
	}
	return nil
}

func (a *App) initMainServer(ctx context.Context) error {
	serverReady := make(chan struct{})
	var readyOnce sync.Once

	// init server (htt,grpc)
	a.mainServer = server.NewServer(
		config.Instance().GrpcServer.Port,
		server.WithHTTPPort(config.Instance().HttpServer.Port),
		server.WithHTTPMiddlewares(traces.HTTP, func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// HTTP Serve starts after both Clay servers have been initialized.
				readyOnce.Do(func() {
					close(serverReady)
				})
				next.ServeHTTP(w, r)
			})
		}),
		server.WithGRPCOpts(
			grpc.StatsHandler(traces.ServerHandler()),
			grpc.StatsHandler(metrics.ServerHandler()),
			grpc.InTapHandle(metrics.TapHandler),
			grpc.ChainUnaryInterceptor(intercept.ErrorInterceptor()),
			grpc.KeepaliveParams(keepalive.ServerParameters{
				MaxConnectionIdle: config.Instance().GrpcServer.MaxConnectionIdle,
				MaxConnectionAge:  config.Instance().GrpcServer.MaxConnectionAge,
				Time:              config.Instance().GrpcServer.Time,
				Timeout:           config.Instance().GrpcServer.Timeout,
			}),
		),
	)

	a.publicCloser.Add(func() error {
		gracefulCtx, cancel := context.WithTimeout(context.Background(), config.Instance().Graceful.Timeout)
		defer cancel()

		// Synchronize Stop with Clay initialization, including a shutdown before
		// the first external request. The admin listener starts independently.
		client := &http.Client{
			Timeout: 100 * time.Millisecond,
			Transport: &http.Transport{
				DisableKeepAlives: true,
			},
		}
		defer client.CloseIdleConnections()
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
	waiting:
		for {
			select {
			case <-serverReady:
				break waiting
			case <-gracefulCtx.Done():
				return fmt.Errorf("notification: server did not finish initialization: %w", gracefulCtx.Err())
			case <-ticker.C:
				response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/swagger.json", config.Instance().HttpServer.Port))
				if err == nil {
					response.Body.Close()
				}
			}
		}

		done := make(chan struct{})
		go func() {
			err := a.mainServer.Stop(gracefulCtx)
			if err != nil {
				slog.Error("Не удалось остановить основной сервер", "error", err)
			}
			close(done)
		}()

		select {
		case <-done:
			slog.Info("Основной сервер остановлен", "service", "notification")
		case <-gracefulCtx.Done():
			err := fmt.Errorf("notification: error while graceful shutdown server: %w", gracefulCtx.Err())
			// Do not call Stop again: Clay GracefulStop does not honor the context.
			// Returning lets main exit after closers complete.
			return fmt.Errorf("notification: stopped: %w", err)
		}
		return nil
	})

	return nil
}

func (a *App) initAdminServer(ctx context.Context) error {
	// init admin listener
	lis, err := net.Listen("tcp", fmt.Sprintf(":%v", config.Instance().HttpServer.AdminPort))
	if err != nil {
		return fmt.Errorf("failed to init admin listener: %w", err)
	}

	a.adminListener = lis

	// init healthcheck for admin server
	if err = a.initHealthCheck(ctx); err != nil {
		return fmt.Errorf("failed to init healthcheck: %w", err)
	}

	a.adminMux = chi.NewMux()

	a.adminMux.Mount("/debug", chimw.Profiler())

	// register healthcheck
	a.adminMux.HandleFunc(healthcheck.LivenessPath, a.healthCheck.LiveEndpoint)
	a.adminMux.HandleFunc(healthcheck.ReadinessPath, a.healthCheck.ReadyEndpoint)
	a.adminMux.HandleFunc(healthcheck.StartupPath, a.healthCheck.StartupEndpoint)

	return nil
}

func (a *App) initGrpcConn(_ context.Context) error {
	for srv, address := range config.Instance().Targets {
		var err error

		conn, err := grpc.NewClient(address,
			grpc.WithTransportCredentials(insecure.NewCredentials()))

		if err != nil {
			return fmt.Errorf(
				"не удалось инициализировать grpc соединение к %s : %s",
				srv,
				err.Error(),
			)
		}

		a.grpcConn[srv] = conn
		closer.Add(conn.Close)
	}
	return nil
}

func (a *App) initHealthCheck(_ context.Context) error {
	a.healthCheck = healthcheck.NewHandler()

	// поверяю, что нет утечки горутин на старте (как пример)
	a.healthCheck.AddLivenessCheck("goroutines", func() error {
		if runtime.NumGoroutine() < 1000 {
			return nil
		}
		return fmt.Errorf("Превышено допустимое число горутин")
	})

	a.healthCheck.AddStartupCheck("started", func() error {
		if atomic.LoadInt32(&a.started) != 0 {
			return nil
		}
		return fmt.Errorf("Приложение ещё не запущено")
	})

	// readiness - т.к. я уже проинициализировал все компоненты
	a.healthCheck.AddReadinessCheck("started", func() error {
		if atomic.LoadInt32(&a.started) != 0 {
			return nil
		}
		return fmt.Errorf("Приложение ещё не запущено")
	})

	a.healthCheck.AddReadinessCheck("termination", func() error {
		if atomic.LoadInt32(&a.terminated) == 0 {
			return nil
		}
		return fmt.Errorf("Приложение останавливается")
	})

	a.publicCloser.Add(func() error {
		slog.Info(
			"Получен сигнал остановки",
			"service",
			"notification",
			"timeout",
			config.Instance().Graceful.Timeout,
		)

		atomic.StoreInt32(&a.terminated, 1)
		return nil
	})
	return nil
}

func (a *App) initGateway(_ context.Context) error {
	a.gateway = gateway.NewRegistry()
	return nil
}

func (a *App) initUsecases(_ context.Context) error {
	a.usecases = usecase.NewRegistry(a.gateway)
	return nil
}

func (a *App) initHandlers(_ context.Context) error {
	a.eventRouter = event_router.NewEventRouter[string, []byte](
		event_router.WithSkipUnknownEventType[string, []byte](),
	)
	a.eventRouter.RegisterAll(
		booking_confirmed.NewHandler(a.usecases.NotifyConfirmed),
		booking_activated.NewHandler(a.usecases.NotifyActivated),
		booking_canceled.NewHandler(a.usecases.NotifyCanceled),
	)
	return nil
}

func (a *App) initMessageBus(_ context.Context) error {
	a.messageBus = messagebus.NewRegistry(a.eventRouter)
	a.publicCloser.Add(a.messageBus.Stop)
	return nil
}

// initTelemetry runs before instrumented transports and background workers.
func (a *App) initTelemetry(ctx context.Context) error {
	instance, err := telemetry.Init(ctx, config.Instance().Telemetry)
	if err != nil {
		return err
	}
	a.telemetry = instance
	return nil
}
