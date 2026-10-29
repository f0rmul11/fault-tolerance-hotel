package canary

import "testing"

func TestRejectUnsupportedServiceAndPercent(t *testing.T) {
	for _, input := range []Options{
		{Action: "deploy", Service: "hotels"},
		{Action: "step", Service: "review", Percent: -1},
		{Action: "step", Service: "review", Percent: 30},
		{Action: "step", Service: "review", Percent: 101},
		{Action: "unknown", Service: "review"},
	} {
		if err := input.Validate(); err == nil {
			t.Fatalf("accepted invalid options: %+v", input)
		}
	}
}
