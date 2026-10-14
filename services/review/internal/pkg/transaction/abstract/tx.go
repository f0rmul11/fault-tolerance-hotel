package abstract

import "context"

type InitFunc func(ctx context.Context) Transaction

type Transaction interface {
	Commit() error
	Rollback() error
}

type Tx struct {
	tx   Transaction
	args any
}

func NewTx(args any) *Tx {
	return &Tx{
		args: args,
	}
}

func (tx *Tx) WithTx(ctx context.Context, init InitFunc) Transaction {
	if tx.tx == nil {
		tx.tx = init(ctx)
	}
	return tx.tx
}

func (tx *Tx) Commit() error {
	if tx.tx == nil {
		return nil
	}
	return tx.tx.Commit()
}

func (tx *Tx) Rollback() error {
	if tx.tx == nil {
		return nil
	}
	return tx.tx.Rollback()
}

func (tx *Tx) Args() any {
	return tx.args
}
