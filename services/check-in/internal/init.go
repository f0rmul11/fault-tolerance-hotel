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

	"check-in/config"
	checkinapp "check-in/internal/app/checkin/v1"
	"check-in/internal/application/usecase"
	bookingevents "check-in/internal/infrastructure/inbox/booking_events"
	"check-in/internal/infrastructure/inbox/booking_events/booking_activated"
	"check-in/internal/infrastructure/inbox/booking_events/booking_canceled"
	"check-in/internal/infrastructure/inbox/booking_events/booking_confirmed"
	"check-in/internal/infrastructure/inbox/booking_events/checkin_rejected"
	"check-in/internal/infrastructure/messagebus"
	"check-in/internal/infrastructure/outbox/checkin_events"
	"check-in/internal/infrastructure/storage"
	"check-in/internal/pkg/connector/postgres"
	event_router "check-in/internal/pkg/event-router"
	"check-in/internal/pkg/inbox"
	"check-in/internal/pkg/outbox"
	pb "check-in/internal/pkg/pb/check-in/checkin/v1"
	"check-in/internal/pkg/worker"

	"check-in/internal/pkg/grpc/intercept"
	"check-in/internal/pkg/healthcheck"

	chimw "github.com/go-chi/chi/v5/middleware"

	"check-in/internal/app/echo/v1"

	"check-in/internal/pkg/closer"
	"check-in/internal/pkg/grpcdesc"
	"check-in/internal/pkg/telemetry"
	"check-in/internal/pkg/telemetry/metrics"
	"check-in/internal/pkg/telemetry/traces"

	echoV1 "check-in/internal/pkg/pb/check-in/echo/v1"

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
			pb.RegisterCheckinServiceServer(
				server,
				checkinapp.New(a.usecases),
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
		atomic.StoreInt32(&a.terminated, 1)
		gracefulCtx, cancel := context.WithTimeout(context.Background(), config.Instance().Graceful.Timeout)
		defer cancel()

		slog.Info("Ожидание прекращения нового трафика", "delay", config.Instance().Graceful.Delay)
		time.Sleep(config.Instance().Graceful.Delay)

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
				return fmt.Errorf("check-in: server did not finish initialization: %w", gracefulCtx.Err())
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
			slog.Info("Основной сервер остановлен", "service", "check-in")
		case <-gracefulCtx.Done():
			err := fmt.Errorf("check-in: error while graceful shutdown server: %w", gracefulCtx.Err())
			// Do not call Stop again: Clay GracefulStop does not honor the context.
			// Returning lets main exit after closers complete.
			return fmt.Errorf("check-in: stopped: %w", err)
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
			"check-in",
			"timeout",
			config.Instance().Graceful.Timeout,
		)

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
	handlerConfig := config.Instance().OutboxConfig(config.CheckinEventsTopic)
	a.outbox.RegisterHandler(checkin_events.NewHandler(
		config.CheckinEventsTopic,
		handlerConfig.BatchSize,
		a.messageBus.Producers.CheckinEvents,
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

func (a *App) initInbox(_ context.Context) error {
	a.inbox = inbox.NewInbox(a.pool, config.Instance().Inbox.Config)
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

func (a *App) initUsecases(_ context.Context) error {
	location, err := time.LoadLocation(config.Instance().Timezone)
	if err != nil {
		return err
	}
	a.usecases = usecase.NewRegistry(a.storage.CheckIns, a.outbox, location)
	return nil
}
func (a *App) initHandlers(_ context.Context) error {
	a.eventRouter = event_router.NewEventRouter[string, []byte]()
	a.eventRouter.RegisterAll(
		booking_confirmed.NewHandler(a.usecases.RegisterCheckIn),
		booking_activated.NewHandler(a.usecases.ConfirmArrival),
		booking_canceled.NewHandler(a.usecases.ConfirmCancellation),
		checkin_rejected.NewHandler(a.usecases.RejectCheckIn),
	)
	a.inbox.RegisterHandler(bookingevents.NewHandler(
		config.BookingEventsTopic,
		config.Instance().InboxConfig(config.BookingEventsTopic).BatchSize,
		a.eventRouter,
	))
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
