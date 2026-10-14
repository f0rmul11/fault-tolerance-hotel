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

	"api-gateway/config"
	booking "api-gateway/internal/app/booking/v1"
	checkin "api-gateway/internal/app/checkin/v1"
	"api-gateway/internal/app/echo/v1"
	hotels "api-gateway/internal/app/hotels/v1"
	"api-gateway/internal/pkg/auth"
	"api-gateway/internal/pkg/closer"
	"api-gateway/internal/pkg/grpc/intercept"
	"api-gateway/internal/pkg/healthcheck"
	"api-gateway/internal/pkg/httptransport"
	bookingV1 "api-gateway/internal/pkg/pb/api-gateway/booking/v1"
	checkinV1 "api-gateway/internal/pkg/pb/api-gateway/checkin/v1"
	echoV1 "api-gateway/internal/pkg/pb/api-gateway/echo/v1"
	hotelsV1 "api-gateway/internal/pkg/pb/api-gateway/hotels/v1"
	externalV1 "api-gateway/internal/pkg/pb/external/booking/booking/v1"
	externalCheckinV1 "api-gateway/internal/pkg/pb/external/check-in/checkin/v1"
	externalHotelsV1 "api-gateway/internal/pkg/pb/external/hotels/hotels/v1"
	"api-gateway/internal/pkg/requestid"
)

func (a *App) initControllers(_ context.Context) error {
	a.controllers = []transport.ServiceDesc{
		hotelsV1.NewHotelsServiceServiceDesc(hotels.NewHotelsService(externalHotelsV1.NewHotelsServiceClient(a.grpcConn[config.HotelsService]))),
		checkinV1.NewCheckinServiceServiceDesc(checkin.NewCheckinService(externalCheckinV1.NewCheckinServiceClient(a.grpcConn[config.CheckinService]))),
		echoV1.NewEchoServiceServiceDesc(echo.NewEchoService()),
		bookingV1.NewBookingServiceServiceDesc(booking.NewBookingService(
			externalV1.NewBookingServiceClient(a.grpcConn[config.BookingService]),
		)),
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
		server.WithHTTPMiddlewares(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// HTTP Serve starts after both Clay servers have been initialized.
				readyOnce.Do(func() {
					close(serverReady)
				})
				next.ServeHTTP(w, r)
			})
		}),
		server.WithRuntimeServeMuxOpts(httptransport.Options()...),
		server.WithGRPCUnaryMiddlewares(
			requestid.Interceptor(),
			auth.Interceptor(),
		),
		server.WithGRPCOpts(
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
				return fmt.Errorf("api-gateway: server did not finish initialization: %w", gracefulCtx.Err())
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
					"api-gateway",
					"error",
					err,
				)
			}
			close(done)
		}()

		select {
		case <-done:
			slog.Warn("api-gateway: main server gracefully stopped")
		case <-gracefulCtx.Done():
			err := fmt.Errorf("api-gateway: error while graceful shutdown server: %w", gracefulCtx.Err())
			// Do not call Stop again: Clay GracefulStop does not honor the context.
			// Returning lets main exit after closers complete.
			return fmt.Errorf("api-gateway: stopped: %w", err)
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
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithUnaryInterceptor(intercept.ProxyMetadata()),
		)

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
		slog.Info("Получен сигнал завершения", "service", "api-gateway",
			"graceful_timeout", config.Instance().Graceful.Timeout)

		atomic.StoreInt32(&a.terminated, 1)
		return nil
	})
	return nil
}
