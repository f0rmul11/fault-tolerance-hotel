package pushback

import (
	"reflect"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"
)

func TestPushbackParse(t *testing.T) {
	tests := []struct {
		name string
		md   metadata.MD
		want Pushback
	}{
		{
			name: "nil metadata → no pushback",
			md:   nil,
			want: Pushback{},
		},
		{
			name: "no header → no pushback",
			md:   metadata.Pairs("other", "123"),
			want: Pushback{},
		},
		{
			name: "-1 → reject",
			md:   metadata.Pairs("grpc-retry-pushback-ms", "-1"),
			want: Pushback{
				Has:    true,
				Reject: true,
			},
		},
		{
			name: "delay = 150ms",
			md:   metadata.Pairs("grpc-retry-pushback-ms", "150"),
			want: Pushback{
				Has:   true,
				Delay: 150 * time.Millisecond,
			},
		},
		{
			name: "invalid number → reject",
			md:   metadata.Pairs("grpc-retry-pushback-ms", "not-num"),
			want: Pushback{
				Has:    true,
				Reject: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Parse(tt.md)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got: %#v want: %#v", got, tt.want)
			}
		})
	}
}

func TestPushbackBoundaries(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		want   Pushback
	}{
		{
			"zero",
			[]string{"0"},
			Pushback{Has: true},
		},
		{
			"empty",
			[]string{""},
			Pushback{
				Has:    true,
				Reject: true,
			},
		},
		{
			"multiple",
			[]string{"0", "0"},
			Pushback{
				Has:    true,
				Reject: true,
			},
		},
		{
			"max int32",
			[]string{"2147483647"},
			Pushback{
				Has:   true,
				Delay: 2147483647 * time.Millisecond,
			},
		},
		{
			"overflow",
			[]string{"2147483648"},
			Pushback{
				Has:    true,
				Reject: true,
			},
		},
		{
			"duration overflow",
			[]string{"9223372036854775807"},
			Pushback{
				Has:    true,
				Reject: true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Parse(metadata.MD{"grpc-retry-pushback-ms": tt.values}); got != tt.want {
				t.Fatalf("got=%+v want=%+v", got, tt.want)
			}
		})
	}
}
