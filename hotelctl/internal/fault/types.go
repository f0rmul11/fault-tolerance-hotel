// Package fault controls bounded transport failures in the course Envoy proxy.
package fault

import (
	"fmt"
	"time"
)

const (
	ControlPort = 9901
	MaxSlowdown = time.Minute
	MaxDuration = time.Hour
)

type Edge struct {
	From string
	To   string
	Port int
}

func (e Edge) DelayPort() int {
	return e.Port + 1000
}

// Edges lists actual synchronous RPC dependencies, each on its own listener.
func Edges() []Edge {
	return []Edge{
		{From: "api-gateway", To: "booking", Port: 10001},
		{From: "api-gateway", To: "hotels", Port: 10002},
		{From: "api-gateway", To: "check-in", Port: 10003},
		{From: "booking", To: "hotels", Port: 10004},
		{From: "hotels", To: "review", Port: 10005},
	}
}

func FindEdge(from, to string) (Edge, error) {
	for _, edge := range Edges() {
		if edge.From == from && edge.To == to {
			return edge, nil
		}
	}
	return Edge{}, fmt.Errorf("нет RPC-связи %s → %s; доступны api-gateway → booking/hotels/check-in, booking → hotels, hotels → review", from, to)
}

type Options struct {
	From         string
	To           string
	Direction    string
	Slowdown     time.Duration
	Duration     time.Duration
	Unavailable  bool
	PushbackMS   *int32
	DelayProfile []DelayStep
	// Handler delay grows with concurrent admitted GetHotelRatings calls per Review process.
	DelayPerInflight  time.Duration
	MaxDelay          time.Duration
	InflightThreshold int
	DelayRamp         time.Duration
	ActivateOnEnter   bool
	// Percent is nil for the default 100%; explicit zero disables injection.
	Percent *int
	Clear   bool
}

func (o Options) Validate() error {
	if o.Percent != nil && (*o.Percent < 0 || *o.Percent > 100) {
		return fmt.Errorf("--percent должен быть от 0 до 100")
	}
	if _, err := FindEdge(o.From, o.To); err != nil {
		return err
	}
	if o.Clear {
		if o.Slowdown != 0 || o.Duration != 0 || o.Unavailable || o.Direction != "request" || o.PushbackMS != nil || o.Percent != nil || len(o.DelayProfile) != 0 || o.DelayPerInflight != 0 || o.MaxDelay != 0 || o.controlledInflight() {
			return fmt.Errorf("--clear не совмещается с настройками сбоя")
		}
		return nil
	}
	if o.PushbackMS != nil {
		if !o.Unavailable || o.Direction != "request" {
			return fmt.Errorf("--pushback-ms требует --unavailable и --direction request")
		}
		if *o.PushbackMS < -1 {
			return fmt.Errorf("--pushback-ms должен быть -1 или неотрицательным int32")
		}
	}
	if o.Direction != "request" && o.Direction != "response" && o.Direction != "handler" {
		return fmt.Errorf("--direction должен быть request, response или handler")
	}
	if o.Direction == "handler" && (o.From != "hotels" || o.To != "review") {
		return fmt.Errorf("--direction handler поддерживается только для hotels → review")
	}
	if o.controlledInflight() && o.DelayPerInflight == 0 {
		return fmt.Errorf("--inflight-threshold, --delay-ramp и --activate-on-enter требуют --delay-per-inflight")
	}
	if o.InflightThreshold < 0 || o.InflightThreshold > 1_000_000 {
		return fmt.Errorf("--inflight-threshold должен быть от 0 до 1000000 (0: порог по умолчанию 1)")
	}
	if o.DelayRamp < 0 || o.DelayRamp > o.Duration || o.DelayRamp%time.Millisecond != 0 {
		return fmt.Errorf("--delay-ramp должен быть не больше --duration, с точностью до миллисекунд")
	}
	if o.DelayPerInflight != 0 || o.MaxDelay != 0 {
		if o.Direction != "handler" || o.Unavailable || len(o.DelayProfile) != 0 || o.PushbackMS != nil {
			return fmt.Errorf("--delay-per-inflight требует --direction handler и --slowdown; не совмещается с другими видами сбоя")
		}
		if o.DelayPerInflight < time.Millisecond || o.DelayPerInflight > MaxSlowdown || o.DelayPerInflight%time.Millisecond != 0 {
			return fmt.Errorf("--delay-per-inflight должен быть от 1ms до %s, с точностью до миллисекунды", MaxSlowdown)
		}
		if o.MaxDelay < time.Millisecond || o.MaxDelay < o.Slowdown || o.MaxDelay > MaxSlowdown || o.MaxDelay%time.Millisecond != 0 {
			return fmt.Errorf("--max-delay должен быть от 1ms до %s и не меньше --slowdown, с точностью до миллисекунды", MaxSlowdown)
		}
	}
	if len(o.DelayProfile) != 0 {
		if o.From != "api-gateway" || o.To != "booking" || o.Direction != "request" {
			return fmt.Errorf("--delay-profile поддерживается для api-gateway → booking, --direction request")
		}
		if o.Slowdown != 0 || o.Unavailable || o.Percent != nil || o.PushbackMS != nil {
			return fmt.Errorf("--delay-profile нельзя совмещать с --slowdown, --percent, --unavailable или --pushback-ms")
		}
		if err := validateProfile(o.DelayProfile); err != nil {
			return err
		}
	} else if o.Unavailable {
		if o.Slowdown != 0 {
			return fmt.Errorf("--unavailable нельзя совмещать с --slowdown")
		}
		if o.Direction == "response" {
			return fmt.Errorf("--unavailable поддерживает --direction request или handler")
		}
	} else if o.Slowdown < time.Millisecond || o.Slowdown > MaxSlowdown || o.Slowdown%time.Millisecond != 0 {
		return fmt.Errorf("укажите --unavailable или --slowdown от 1ms до %s, с точностью до миллисекунды", MaxSlowdown)
	}
	if o.Duration < time.Millisecond || o.Duration > MaxDuration || o.Duration%time.Millisecond != 0 {
		return fmt.Errorf("--duration должен быть от 1ms до %s, с точностью до миллисекунды", MaxDuration)
	}
	return nil
}

func (o Options) controlledInflight() bool {
	return o.InflightThreshold != 0 || o.DelayRamp != 0 || o.ActivateOnEnter
}

// Percentage preserves the existing whole-link fault when the flag is omitted.
func (o Options) Percentage() int {
	if o.Percent != nil {
		return *o.Percent
	}
	return 100
}
