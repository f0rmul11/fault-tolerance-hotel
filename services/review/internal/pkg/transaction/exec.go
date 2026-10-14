package transaction

import (
	"context"
	"errors"

	"review/internal/pkg/transaction/abstract"
)

func Exec(ctx context.Context, callback func(context.Context) error) (err error) {
	if abstract.TxFromContext(ctx) != nil {
		return callback(ctx)
	}
	ctx, finish := abstract.ContextWithTx(ctx, nil)
	defer func() {
		if p := recover(); p != nil {
			_ = finish(errors.New("паника в транзакции"))
			panic(p)
		}
		err = finish(err)
	}()
	return callback(ctx)
}
