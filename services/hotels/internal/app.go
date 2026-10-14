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

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/not-for-prod/clay/server"
	"github.com/not-for-prod/clay/transport"
	"google.golang.org/grpc"

	"hotels/config"
	"hotels/internal/application/service"
	"hotels/internal/application/usecase"
	"hotels/internal/infrastructure/gateway"
	"hotels/internal/infrastructure/storage"
	"hotels/internal/pkg/closer"
	"hotels/internal/pkg/healthcheck"
)

// App application
type App struct {
	gateway    *gateway.Registry
	services   *service.Registry
	pool       *pgxpool.Pool
	storage    *storage.Registry
	usecases   *usecase.Registry
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

// New конструктор
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
func (a *App) Run(_ context.Context) {
	if a.mainServer != nil {
		go func() {
			if err := a.mainServer.Run(a.controllers...); err != nil {
				slog.Error("Ошибка основного сервера", "error", err)
				a.publicCloser.CloseAll()
			}
		}()
	}

	// start signal
	atomic.StoreInt32(&a.started, 1)

	slog.Info("Приложение запущено",
		"service", "hotels",
		"grpc_port", config.Instance().GrpcServer.Port,
		"http_port", config.Instance().HttpServer.Port,
	)

	a.publicCloser.Wait()

	closer.CloseAll()

	a.adminCloser.CloseAll()
}

func (a *App) init(ctx context.Context) error {
	initFuncs := []func(context.Context) error{
		a.initMainServer,
		a.initGrpcConn,
		a.initPostgres,
		a.initStorage,
		a.initGateway,
		a.initServices,
		a.initUsecases,
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
