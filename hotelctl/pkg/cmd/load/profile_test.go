package load

import (
	"reflect"
	"testing"
	"time"
)

func TestLoadProfiles(t *testing.T) {
	tests := []struct {
		name    string
		profile string
		to      int
		want    []string
	}{
		{name: "existing default", want: []string{"const", "10", "--duration=2m0s"}},
		{name: "constant", profile: "const", want: []string{"const", "10", "--duration=2m0s"}},
		{name: "linear", profile: "line", to: 150, want: []string{"line", "10", "150", "2m0s"}},
		{name: "missing endpoint", profile: "line"},
		{name: "flat line", profile: "line", to: 10},
		{name: "decreasing", profile: "line", to: 5},
		{name: "endpoint too high", profile: "line", to: 1000001},
		{name: "endpoint without line", profile: "const", to: 150},
		{name: "unknown profile", profile: "linear", to: 150},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := options{
				addr:     "localhost:9090",
				method:   "example.Service/Read",
				data:     "{}",
				rps:      10,
				rpsTo:    tt.to,
				profile:  tt.profile,
				duration: 2 * time.Minute,
				clients:  4,
			}
			_, _, err := o.validate()
			if (err == nil) != (tt.want != nil) {
				t.Fatalf("validation: %v", err)
			}
			if err != nil {
				return
			}
			want := append([]string{
				"load", "--addr=localhost:9090", "--requests-file=requests.bin",
				"--inmem-requests", "--clients=4", "--phout=phout.tsv",
			}, tt.want...)
			if got := o.loadArgs("requests.bin", "phout.tsv"); !reflect.DeepEqual(got, want) {
				t.Fatalf("Framer command: %v; want %v", got, want)
			}
		})
	}
}
