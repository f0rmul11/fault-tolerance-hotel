package wrapper

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"hotels/internal/pkg/transaction/abstract"
)

type Executor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Database interface {
	Pool(ctx context.Context) Executor
}

type database struct {
	pool *pgxpool.Pool
}

func (db *database) Pool(ctx context.Context) Executor {
	if tx := abstract.TxFromContext(ctx); tx != nil {
		return initTx(ctx, tx, db.pool)
	}
	return db.pool
}

func NewDatabase(pool *pgxpool.Pool) Database {
	return &database{
		pool: pool,
	}
}

func initTx(ctx context.Context, tx *abstract.Tx, pool *pgxpool.Pool) Executor {
	return tx.WithTx(ctx, func(ctx context.Context) abstract.Transaction {
		var opts *pgx.TxOptions
		if tx.Args() != nil {
			opts = tx.Args().(*pgx.TxOptions)
		}

		return NewTransaction(ctx, pool, opts)
	}).(Executor)
}
