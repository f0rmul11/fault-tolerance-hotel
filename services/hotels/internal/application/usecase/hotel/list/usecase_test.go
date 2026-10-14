package list

import (
	"context"
	"testing"

	"github.com/samber/mo"

	entity "hotels/internal/domain/entity/hotel"
)

type dependencies struct {
	limit, offset, calls int
}

func (d *dependencies) List(_ context.Context, limit, offset int) ([]*entity.Hotel, error) {
	d.limit = limit
	d.offset = offset
	d.calls++
	return nil, nil
}

func (d *dependencies) Hotels(context.Context, []*entity.Hotel) error {
	return nil
}

func TestPagination(t *testing.T) {
	for _, tt := range []struct {
		name    string
		input   Input
		want    int
		invalid bool
	}{
		{
			name: "default",
			want: 20,
		},
		{
			name: "maximum",
			input: Input{
				Limit:  mo.Some(100),
				Offset: 3,
			},
			want: 100,
		},
		{
			name:    "zero",
			input:   Input{Limit: mo.Some(0)},
			invalid: true,
		},
		{
			name:    "too large",
			input:   Input{Limit: mo.Some(101)},
			invalid: true,
		},
		{
			name:    "negative offset",
			input:   Input{Offset: -1},
			invalid: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d := &dependencies{}
			_, err := New(d, d).Execute(context.Background(), tt.input)
			if (err != nil) != tt.invalid {
				t.Fatalf("error = %v", err)
			}
			if tt.invalid && d.calls != 0 {
				t.Fatal("invalid input reached storage")
			}
			if !tt.invalid && (d.calls != 1 || d.limit != tt.want || d.offset != tt.input.Offset) {
				t.Fatalf("wrong pagination: %+v", d)
			}
		})
	}
}
