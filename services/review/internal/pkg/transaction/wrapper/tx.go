package wrapper

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/lo"

	"review/internal/pkg/transaction/abstract"
)

type Transaction interface {
	abstract.Transaction
	Executor
}

type Tx struct {
	pool *pgxpool.Pool
	ctx  context.Context
	tx   pgx.Tx
	err  error
	opts pgx.TxOptions
}

func NewTransaction(ctx context.Context, pool *pgxpool.Pool, opts *pgx.TxOptions) Transaction {
	if opts == nil {
		opts = &pgx.TxOptions{
			IsoLevel: pgx.ReadCommitted,
		}
	}

	return &Tx{
		ctx:  ctx,
		pool: pool,
		opts: lo.FromPtr(opts),
	}
}

func (t *Tx) Commit() error {
	if t.tx == nil {
		return nil
	}

	t.err = nil
	return t.tx.Commit(t.ctx)
}

func (t *Tx) Rollback() error {
	if t.tx == nil {
		return nil
	}

	t.err = nil
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.ctx), 5*time.Second)
	defer cancel()
	return t.tx.Rollback(ctx)
}

func (t *Tx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	t.begin()
	if t.err != nil {
		return pgconn.CommandTag{}, t.err
	}
	return t.tx.Exec(ctx, sql, args...)
}

func (t *Tx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	t.begin()
	if t.err != nil {
		return nil, t.err
	}
	return t.tx.Query(ctx, sql, args...)
}

func (t *Tx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	t.begin()
	if t.err != nil {
		return &txErrRow{
			err: t.err,
		}
	}
	return t.tx.QueryRow(ctx, sql, args...)
}

func (t *Tx) begin() {
	if t.tx == nil && t.err == nil {
		tx, err := t.pool.BeginTx(t.ctx, t.opts)
		if err != nil {
			t.err = err
			return
		}
		t.tx = tx
	}
}
