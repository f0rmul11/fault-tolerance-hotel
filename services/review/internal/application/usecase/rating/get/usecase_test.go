package get

import (
	"context"
	"testing"

	"github.com/gofrs/uuid"

	entity "review/internal/domain/entity/rating"
)

type storage struct {
	calls int
	ids   []uuid.UUID
}

func (s *storage) Get(_ context.Context, ids []uuid.UUID) ([]*entity.Rating, error) {
	s.calls++
	s.ids = ids
	return nil, nil
}

func TestBatchInput(t *testing.T) {
	id := uuid.FromStringOrNil("cdd4685e-c610-4e7e-95ee-c51fb6807400")
	many := make([]uuid.UUID, 101)
	for i := range many {
		many[i] = id
	}
	for _, tt := range []struct {
		name    string
		ids     []uuid.UUID
		invalid bool
		calls   int
	}{
		{name: "empty"},
		{
			name:  "batch",
			ids:   []uuid.UUID{id, id},
			calls: 1,
		},
		{
			name:    "invalid id",
			ids:     []uuid.UUID{uuid.Nil},
			invalid: true,
		},
		{
			name:  "batch larger than 100",
			ids:   many,
			calls: 1,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := &storage{}
			_, err := New(s).Execute(context.Background(), tt.ids)
			if (err != nil) != tt.invalid || s.calls != tt.calls {
				t.Fatalf("calls=%d err=%v", s.calls, err)
			}
			if s.calls > 0 && len(s.ids) != len(tt.ids) {
				t.Fatal("batch was split")
			}
		})
	}
}
