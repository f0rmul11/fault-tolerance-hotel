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

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/not-for-prod/clay/server"
	"github.com/not-for-prod/clay/transport"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"

	"hotels/config"
	"hotels/internal/app/echo/v1"
	hotelsapp "hotels/internal/app/hotels/v1"
	"hotels/internal/application/service"
	"hotels/internal/application/usecase"
	runtimeconfig "hotels/internal/config"
	"hotels/internal/infrastructure/gateway"
	"hotels/internal/infrastructure/storage"
	"hotels/internal/pkg/closer"
	"hotels/internal/pkg/connector/postgres"
	"hotels/internal/pkg/grpc/intercept"
	"hotels/internal/pkg/grpcdesc"
	"hotels/internal/pkg/healthcheck"
	echoV1 "hotels/internal/pkg/pb/hotels/echo/v1"
	businessV1 "hotels/internal/pkg/pb/hotels/hotels/v1"
	"hotels/internal/pkg/rtconfig/global"
	"hotels/internal/pkg/telemetry"
	"hotels/internal/pkg/telemetry/metrics"
	"hotels/internal/pkg/telemetry/traces"
)

func (a *App) initControllers(_ context.Context) error {
	a.controllers = []transport.ServiceDesc{
		grpcdesc.New(func(server *grpc.Server) {
			echoV1.RegisterEchoServiceServer(server, echo.NewEchoService())
			businessV1.RegisterHotelsServiceServer(server, hotelsapp.New(a.usecases))
		}),
	}
	return nil
}

func (a *App) initMainServer(_ context.Context) error {
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

	return nil
}

func (a *App) initRuntimeConfig(ctx context.Context) error {
	client, err := global.Init(ctx, config.Instance().RuntimeConfig, runtimeconfig.SchemaHash)
	if err != nil {
		return err
	}
	a.runtimeConfig.Store(client)
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
	a.healthCheck.AddReadinessCheck("runtime_config", func() error {
		return a.runtimeConfig.Load().CheckInitialSync()
	})

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
			"hotels",
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

func (a *App) initStorage(context.Context) error {
	a.storage = storage.NewRegistry(a.pool)
	return nil
}

func (a *App) initUsecases(context.Context) error {
	a.usecases = usecase.NewRegistry(
		a.storage,
		a.services,
		a.gateway,
		runtimeconfig.NewClient(a.runtimeConfig.Load()),
	)
	return nil
}

func (a *App) initGateway(context.Context) error {
	a.gateway = gateway.NewRegistry(a.grpcConn)
	return nil
}

func (a *App) initServices(context.Context) error {
	a.services = service.NewRegistry(a.gateway)
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
