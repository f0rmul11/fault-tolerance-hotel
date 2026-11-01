package throttler

import "testing"

func TestThrottler(t *testing.T) {
	tests := []struct {
		name    string
		setup   func() *Throttler
		actions func(*Throttler) (throttle bool)
		want    bool
	}{
		{
			name: "initial → no throttle",
			setup: func() *Throttler {
				return NewThrottler(10, 1)
			},
			actions: func(th *Throttler) bool {
				return th.Throttle()
			},
			want: false,
		},
		{
			name: "exhaust tokens → throttle",
			setup: func() *Throttler {
				return NewThrottler(2, 1)
			},
			actions: func(th *Throttler) bool {
				th.Throttle()        // 1
				th.Throttle()        // 0
				return th.Throttle() // <= thresh → throttle
			},
			want: true,
		},
		{
			name: "SuccessfulRPC refills tokens → no throttle",
			setup: func() *Throttler {
				return NewThrottler(3, 2)
			},
			actions: func(th *Throttler) bool {
				th.Throttle()        // 2
				th.Throttle()        // 1
				th.Throttle()        // 0
				th.SuccessfulRPC()   // 0 -> 2
				th.SuccessfulRPC()   // 2 -> 3 (cap)
				return th.Throttle() // 3 -> 2 > 1.5
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			th := tt.setup()
			got := tt.actions(th)
			if got != tt.want {
				t.Fatalf("got: %v want: %v", got, tt.want)
			}
		})
	}
}

func TestThresholdAndRecovery(t *testing.T) {
	th := NewThrottler(10, 1)
	for i := 0; i < 12; i++ {
		blocked := th.Throttle()
		if blocked != (i >= 4) {
			t.Fatalf("failure %d: blocked=%v tokens=%v", i+1, blocked, th.tokens)
		}
	}
	if th.tokens != 0 {
		t.Fatalf("tokens=%v", th.tokens)
	}
	for i := 0; i < 6; i++ {
		th.SuccessfulRPC()
	}
	if !th.Throttle() || th.tokens != 5 {
		t.Fatal("6 -> 5 must still suppress retry")
	}
	th.SuccessfulRPC()
	th.SuccessfulRPC()
	if th.Throttle() || th.tokens != 6 {
		t.Fatal("7 -> 6 must permit retry")
	}
	for i := 0; i < 20; i++ {
		th.SuccessfulRPC()
	}
	if th.tokens != 10 {
		t.Fatalf("refill exceeded max: %v", th.tokens)
	}
}
