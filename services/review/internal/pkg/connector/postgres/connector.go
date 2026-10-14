package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"review/config"
	"review/internal/pkg/closer"
)

func Pool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	// конфигурируем пул
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}

	cfg.MaxConns = config.Instance().Postgres.MaxConnections
	cfg.MinConns = config.Instance().Postgres.MinConnections
	cfg.MinIdleConns = config.Instance().Postgres.MaxIdleConnections
	cfg.MaxConnLifetime = config.Instance().Postgres.MaxConnLifetime

	// создаем пул
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}

	closer.Add(func() error {
		pool.Close()
		return nil
	})

	// ping
	if err = pool.Ping(ctx); err != nil {
		return nil, err
	}

	return pool, nil
}
