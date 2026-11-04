package fault

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// DelayStep задаёт долю физических попыток и добавленную задержку.
// Группы профиля взаимоисключающие; остаток до 100% проходит без задержки.
type DelayStep struct {
	Percent int
	Delay   time.Duration
}

// ParseDelayProfile читает профиль вида "8:150ms,2:800ms".
func ParseDelayProfile(value string) ([]DelayStep, error) {
	var steps []DelayStep
	for _, part := range strings.Split(value, ",") {
		percent, delay, ok := strings.Cut(strings.TrimSpace(part), ":")
		if !ok {
			return nil, fmt.Errorf("--delay-profile: ожидается процент:задержка, например 8:150ms,2:800ms")
		}
		n, err := strconv.Atoi(percent)
		if err != nil {
			return nil, fmt.Errorf("--delay-profile: неверная доля %q", percent)
		}
		duration, err := time.ParseDuration(delay)
		if err != nil {
			return nil, fmt.Errorf("--delay-profile: неверная задержка %q", delay)
		}
		steps = append(steps, DelayStep{Percent: n, Delay: duration})
	}
	return steps, validateProfile(steps)
}

func validateProfile(steps []DelayStep) error {
	total := 0
	for _, step := range steps {
		if step.Percent < 1 || step.Percent > 100 {
			return fmt.Errorf("--delay-profile: доля каждой группы должна быть от 1 до 100%%")
		}
		if step.Delay < time.Millisecond || step.Delay > MaxSlowdown || step.Delay%time.Millisecond != 0 {
			return fmt.Errorf("--delay-profile: задержка должна быть от 1ms до %s с точностью до миллисекунды", MaxSlowdown)
		}
		total += step.Percent
	}
	if total > 100 {
		return fmt.Errorf("--delay-profile: сумма долей превышает 100%%")
	}
	return nil
}
