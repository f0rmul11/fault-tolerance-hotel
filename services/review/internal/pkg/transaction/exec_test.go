package transaction

import (
	"context"
	"errors"
	"testing"

	"review/internal/pkg/transaction/abstract"
)

type fakeTx struct {
	commits   int
	rollbacks int
	commitErr error
}

func (f *fakeTx) Commit() error {
	f.commits++
	return f.commitErr
}

func (f *fakeTx) Rollback() error {
	f.rollbacks++
	return nil
}

func TestExec(t *testing.T) {
	sentinel := errors.New("failure")
	for _, tt := range []struct {
		name        string
		callbackErr error
		commitErr   error
		commits     int
		rollbacks   int
	}{
		{
			"success",
			nil,
			nil,
			1,
			0,
		},
		{
			"callback error",
			sentinel,
			nil,
			0,
			1,
		},
		{
			"commit error",
			nil,
			sentinel,
			1,
			0,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tx := &fakeTx{
				commitErr: tt.commitErr,
			}
			err := Exec(context.Background(), func(ctx context.Context) error {
				abstract.TxFromContext(ctx).WithTx(ctx, func(context.Context) abstract.Transaction {
					return tx
				})
				return Exec(ctx, func(context.Context) error {
					return tt.callbackErr
				})
			})
			if tx.commits != tt.commits || tx.rollbacks != tt.rollbacks {
				t.Fatalf("commit=%d rollback=%d", tx.commits, tx.rollbacks)
			}
			if (tt.callbackErr != nil || tt.commitErr != nil) && !errors.Is(err, sentinel) {
				t.Fatalf("lost cause: %v", err)
			}
		})
	}
}

func TestPanicRollsBack(t *testing.T) {
	tx := &fakeTx{}
	defer func() {
		if recover() != "panic" || tx.rollbacks != 1 || tx.commits != 0 {
			t.Fatalf("unexpected tx: %+v", tx)
		}
	}()
	_ = Exec(context.Background(), func(ctx context.Context) error {
		abstract.TxFromContext(ctx).WithTx(ctx, func(context.Context) abstract.Transaction {
			return tx
		})
		panic("panic")
	})
}
