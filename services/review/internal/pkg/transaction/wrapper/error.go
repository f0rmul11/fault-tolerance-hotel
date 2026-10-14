package wrapper

type txErrRow struct {
	err error
}

func (t txErrRow) Scan(...any) error {
	return t.err
}
