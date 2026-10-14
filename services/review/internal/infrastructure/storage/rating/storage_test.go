package rating

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"

	"review/internal/pkg/transaction/wrapper"
)

type executor struct {
	wrapper.Executor
	calls int
	ids   []uuid.UUID
	query string
}

func (e *executor) Query(_ context.Context, query string, args ...any) (pgx.Rows, error) {
	e.calls++
	e.query = query
	e.ids = args[0].([]uuid.UUID)
	return nil, errors.New("query result")
}

type database struct {
	executor *executor
}

func (d database) Pool(context.Context) wrapper.Executor {
	return d.executor
}

func TestGetUsesOneSQLForWholeBatch(t *testing.T) {
	ids := []uuid.UUID{uuid.FromStringOrNil("cdd4685e-c610-4e7e-95ee-c51fb6807400"), uuid.FromStringOrNil("abca2095-b122-4323-bb20-459d2cc86818")}
	e := &executor{}
	_, err := New(database{executor: e}).Get(context.Background(), ids)
	if err == nil || e.calls != 1 || !reflect.DeepEqual(e.ids, ids) || !strings.Contains(e.query, "any($1::uuid[])") {
		t.Fatalf("batch SQL not used: %+v, %v", e, err)
	}
	e.calls = 0
	_, err = New(database{executor: e}).Get(context.Background(), nil)
	if err != nil || e.calls != 0 {
		t.Fatal("empty batch must skip SQL")
	}
}
