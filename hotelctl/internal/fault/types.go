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
	From        string
	To          string
	Direction   string
	Slowdown    time.Duration
	Duration    time.Duration
	Unavailable bool
	Clear       bool
}

func (o Options) Validate() error {
	if _, err := FindEdge(o.From, o.To); err != nil {
		return err
	}
	if o.Clear {
		if o.Slowdown != 0 || o.Duration != 0 || o.Unavailable || o.Direction != "request" {
			return fmt.Errorf("--clear не совмещается с настройками сбоя")
		}
		return nil
	}
	if o.Direction != "request" && o.Direction != "response" {
		return fmt.Errorf("--direction должен быть request или response")
	}
	if o.Unavailable {
		if o.Slowdown != 0 {
			return fmt.Errorf("--unavailable нельзя совмещать с --slowdown")
		}
		if o.Direction != "request" {
			return fmt.Errorf("--unavailable поддерживает только --direction request")
		}
	} else if o.Slowdown < time.Millisecond || o.Slowdown > MaxSlowdown || o.Slowdown%time.Millisecond != 0 {
		return fmt.Errorf("укажите --unavailable или --slowdown от 1ms до %s, с точностью до миллисекунды", MaxSlowdown)
	}
	if o.Duration < time.Millisecond || o.Duration > MaxDuration || o.Duration%time.Millisecond != 0 {
		return fmt.Errorf("--duration должен быть от 1ms до %s, с точностью до миллисекунды", MaxDuration)
	}
	return nil
}
