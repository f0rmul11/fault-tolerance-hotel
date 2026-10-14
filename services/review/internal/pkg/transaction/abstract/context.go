package abstract

import (
	"context"

	"github.com/pkg/errors"
)

type contextTxKey struct{}

var txKey = contextTxKey{}

type CommitFunc func(err error) error

func ContextWithTx(ctx context.Context, args any) (context.Context, CommitFunc) {
	tx := NewTx(args)

	return context.WithValue(ctx, txKey, tx), func(err error) error {
		if err != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				err = errors.Wrap(err, rollbackErr.Error())
			}
			return err
		}
		return tx.Commit()
	}
}

func TxFromContext(ctx context.Context) *Tx {
	if tx, ok := ctx.Value(txKey).(*Tx); ok {
		return tx
	}
	return nil
}
