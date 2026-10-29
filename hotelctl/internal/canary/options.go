package canary

import "fmt"

const (
	Namespace = "hotelflow"
	Name      = "review"
	Service   = "review-rollout"
	Routing   = "review-canary"
)

type Options struct {
	Action  string
	Service string
	Percent int
	Restart bool
}

func (o Options) Validate() error {
	if o.Service != "review" {
		return fmt.Errorf("канареечный режим реализован только для review")
	}
	switch o.Action {
	case "setup", "deploy", "status", "abort", "promote", "disable":
	case "step":
		switch o.Percent {
		case 10, 25, 50, 100:
		default:
			return fmt.Errorf("шаг должен быть одним из: 10, 25, 50, 100")
		}
	default:
		return fmt.Errorf("неизвестная команда canary %q", o.Action)
	}
	return nil
}
