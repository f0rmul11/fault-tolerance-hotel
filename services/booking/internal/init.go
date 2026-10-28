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

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/not-for-prod/clay/server"
	"github.com/not-for-prod/clay/transport"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"

	"booking/config"
	bookingapp "booking/internal/app/booking/v1"
	"booking/internal/app/echo/v1"
	"booking/internal/application/service"
	"booking/internal/application/usecase"
	"booking/internal/infrastructure/gateway"
	"booking/internal/infrastructure/inbox/checkin_events"
	"booking/internal/infrastructure/inbox/checkin_events/arrival_requested"
	"booking/internal/infrastructure/inbox/checkin_events/cancellation_requested"
	"booking/internal/infrastructure/messagebus"
	"booking/internal/infrastructure/outbox/booking_events"
	"booking/internal/infrastructure/storage"
	"booking/internal/pkg/closer"
	"booking/internal/pkg/connector/postgres"
	event_router "booking/internal/pkg/event-router"
	"booking/internal/pkg/grpc/intercept"
	"booking/internal/pkg/grpcdesc"
	"booking/internal/pkg/healthcheck"
	"booking/internal/pkg/inbox"
	"booking/internal/pkg/outbox"
	pb "booking/internal/pkg/pb/booking/booking/v1"
	echoV1 "booking/internal/pkg/pb/booking/echo/v1"
	"booking/internal/pkg/telemetry"
	"booking/internal/pkg/telemetry/metrics"
	"booking/internal/pkg/telemetry/traces"
	"booking/internal/pkg/worker"
)

func (a *App) initControllers(_ context.Context) error {
	a.controllers = []transport.ServiceDesc{
		grpcdesc.New(func(server *grpc.Server) {
			echoV1.RegisterEchoServiceServer(server, echo.NewEchoService())
			pb.RegisterBookingServiceServer(
				server,
				bookingapp.New(a.usecases),
			)
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
			grpc.ChainUnaryInterceptor(intercept.ErrorInterceptor(), intercept.AuthInterceptor()),
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
				return fmt.Errorf("booking: server did not finish initialization: %w", gracefulCtx.Err())
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
				slog.Error(
					"Ошибка остановки основного сервера",
					"service",
					"booking",
					"error",
					err,
				)
			}
			close(done)
		}()
		select {
		case <-done:
			slog.Warn("booking: main server gracefully stopped")
		case <-gracefulCtx.Done():
			err := fmt.Errorf("booking: error while graceful shutdown server: %w", gracefulCtx.Err())
			// Do not call Stop again: Clay GracefulStop does not honor the context.
			// Returning lets main exit after closers complete.
			return fmt.Errorf("booking: stopped: %w", err)
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
			grpc.WithStatsHandler(traces.ClientHandler()),
			grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDisableRetry())
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

		return fmt.Errorf("application has too much running goroutines")
	})
	a.healthCheck.AddStartupCheck("started", func() error {
		if atomic.LoadInt32(&a.started) != 0 {
			return nil
		}

		return fmt.Errorf("application is not started yet")
	})
	// readiness - т.к. я уже проинициализировал все компоненты
	a.healthCheck.AddReadinessCheck("started", func() error {
		if atomic.LoadInt32(&a.started) != 0 {
			return nil
		}

		return fmt.Errorf("application is not statred yet")
	})
	a.healthCheck.AddReadinessCheck("termination", func() error {
		if atomic.LoadInt32(&a.terminated) == 0 {
			return nil
		}

		return fmt.Errorf("application is terminating now")
	})
	a.publicCloser.Add(func() error {
		slog.Info("Получен сигнал завершения", "service", "booking",
			"graceful_timeout", config.Instance().Graceful.Timeout)
		atomic.StoreInt32(&a.terminated, 1)
		return nil
	})
	return nil
}

func (a *App) initPostgres(ctx context.Context) error {
	pool, err := postgres.Pool(ctx, config.Instance().PostgresDSN)
	if err != nil {
		return fmt.Errorf("[POSTGRES] Не удалось инициализировать pool: %w", err)
	}
	a.pool = pool
	return nil
}

func (a *App) initStorage(_ context.Context) error {
	a.storage = storage.NewRegistry(a.pool)
	return nil
}

func (a *App) initMessageBus(_ context.Context) error {
	a.messageBus = messagebus.NewRegistry(a.inbox)
	a.publicCloser.Add(a.messageBus.Stop)
	return nil
}

func (a *App) initOutbox(ctx context.Context) error {
	a.outbox = outbox.NewOutbox(a.pool, config.Instance().Outbox.Limits)
	handlerConfig := config.Instance().OutboxConfig(config.BookingEventsTopic)
	a.outbox.RegisterHandler(booking_events.NewHandler(
		config.BookingEventsTopic,
		handlerConfig.BatchSize,
		a.messageBus.Producers.BookingEvents,
	))
	a.messageRelay = worker.NewWorker(ctx, a.outbox.HandlePendingMessages,
		func(context.Context) time.Duration {
			return handlerConfig.Worker.Interval
		},
		func(context.Context) int {
			return handlerConfig.Worker.Concurrency
		},
	)
	// Stop workers before global closers release the producer and database.
	a.publicCloser.Add(a.messageRelay.Stop)
	return nil
}

func (a *App) initServices(_ context.Context) error {
	a.services = service.NewRegistry(a.storage.Bookings, a.outbox)
	return nil
}

func (a *App) initUsecases(_ context.Context) error {
	a.usecases = usecase.NewRegistry(
		a.storage.Bookings,
		a.gateway.Hotels,
		a.services,
		a.outbox,
	)
	return nil
}

func (a *App) initGateway(_ context.Context) error {
	a.gateway = gateway.NewRegistry(a.grpcConn)
	return nil
}

func (a *App) initInbox(_ context.Context) error {
	a.inbox = inbox.NewInbox(a.pool, config.Instance().Inbox.Config)
	return nil
}
func (a *App) initHandlers(_ context.Context) error {
	a.eventRouter = event_router.NewEventRouter[string, []byte]()
	a.eventRouter.RegisterAll(
		arrival_requested.NewHandler(a.usecases.ActivateBooking),
		cancellation_requested.NewHandler(a.usecases.CancelBooking),
	)
	a.inbox.RegisterHandler(checkin_events.NewHandler(
		config.CheckinEventsTopic,
		config.Instance().InboxConfig(config.CheckinEventsTopic).BatchSize,
		a.eventRouter,
	))
	return nil
}
func (a *App) initInboxWorkers(ctx context.Context) error {
	for _, mode := range []inbox.Mode{inbox.ModeNormal, inbox.ModeError} {
		interval := config.Instance().Inbox.Config.NormalMessagesPollInterval
		if mode == inbox.ModeError {
			interval = config.Instance().Inbox.Config.ErrorMessagesPollInterval
		}
		background := worker.NewWorker(
			ctx,
			a.inbox.HandlePendingMessages(mode),
			func(context.Context) time.Duration {
				return interval
			},
			func(context.Context) int {
				return 1
			},
		)
		a.inboxWorkers = append(a.inboxWorkers, background)
		a.publicCloser.Add(background.Stop)
	}
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
