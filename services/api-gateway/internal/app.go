package internal

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"syscall"
	"time"

	"api-gateway/config"
	"api-gateway/internal/pkg/closer"
	"api-gateway/internal/pkg/healthcheck"
	"api-gateway/internal/pkg/telemetry"

	"github.com/go-chi/chi/v5"
	"github.com/not-for-prod/clay/server"
	"github.com/not-for-prod/clay/transport"
	"google.golang.org/grpc"
)

// App application
type App struct {
	telemetry  *telemetry.Telemetry
	mainServer *server.Server

	adminListener net.Listener
	adminMux      *chi.Mux

	controllers []transport.ServiceDesc

	grpcConn map[string]grpc.ClientConnInterface

	started    int32
	terminated int32

	// обработчик health check probe
	healthCheck healthcheck.Handler

	// closers for graceful shutdown
	publicCloser *closer.Closer

	adminCloser *closer.Closer
}

// New создаёт приложение.
func New(ctx context.Context) *App {
	app := &App{
		grpcConn:     make(map[string]grpc.ClientConnInterface),
		publicCloser: closer.New(syscall.SIGTERM, syscall.SIGINT),
		adminCloser:  closer.New(),
	}

	// init admin server
	if err := app.initAdminServer(ctx); err != nil {
		log.Fatalf("[APP] Не удалось инициализировать приложение: %s", err.Error())
	}

	// run admin server before init application
	app.runAdminServer(ctx)

	if err := app.init(ctx); err != nil {
		closer.CloseAll()
		app.adminCloser.CloseAll()
		_ = app.telemetry.Shutdown(ctx)
		log.Fatalf("[APP] Не удалось инициализировать приложение: %s", err.Error())
	}
	return app
}

// runAdminServer запускает служебный сервер (debug endpoint'ы, healthcheck etc.)
func (a *App) runAdminServer(_ context.Context) {
	adminServer := &http.Server{
		Handler: a.adminMux,
	}

	go func() {
		if err := adminServer.Serve(a.adminListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.adminCloser.CloseAll()
		}
	}()

	a.adminCloser.Add(func() error {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		adminServer.SetKeepAlivesEnabled(false)
		if err := adminServer.Shutdown(ctx); err != nil {
			return fmt.Errorf("error during shutdown admin http server: %w", err)
		}
		return nil
	})
}

// Run запуск приложения
func (a *App) Run(ctx context.Context) {
	defer func() {
		a.publicCloser.CloseAll()
		closer.CloseAll()
		a.adminCloser.CloseAll()
		if err := a.telemetry.Shutdown(ctx); err != nil {
			slog.Warn("Не удалось завершить экспорт телеметрии", "error", err)
		}
	}()

	if a.mainServer != nil {
		go func() {
			if err := a.mainServer.Run(a.controllers...); err != nil {
				slog.Error("Ошибка основного сервера", "service", "api-gateway", "error", err)
				a.publicCloser.CloseAll()
			}
		}()
	}

	// start signal
	atomic.StoreInt32(&a.started, 1)

	slog.Info("Сервис запущен", "service", "api-gateway",
		"grpc_port", config.Instance().GrpcServer.Port,
		"http_port", config.Instance().HttpServer.Port,
	)

	a.publicCloser.Wait()
}

func (a *App) init(ctx context.Context) error {
	initFuncs := []func(context.Context) error{
		a.initTelemetry,
		a.initMainServer,
		a.initGrpcConn,
		a.initControllers,
	}

	for _, f := range initFuncs {
		err := f(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}
