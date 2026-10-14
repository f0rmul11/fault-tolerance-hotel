package vo

import (
	"time"

	app "check-in/internal/pkg/checkinerror"
)

// Period — полуоткрытый интервал [start, end), независимо от календарных суток.
type Period struct {
	start time.Time
	end   time.Time
}

func NewPeriod(start, end time.Time) (Period, error) {
	period := Period{
		start: start.UTC(),
		end:   end.UTC(),
	}

	if err := period.Validate(); err != nil {
		return Period{}, err
	}

	return period, nil
}

func ParsePeriod(start, end string) (Period, error) {
	from, err := time.Parse(time.RFC3339Nano, start)
	if err != nil {
		return Period{}, app.NewInvalidArgumentErr("Начало периода должно содержать дату, время и часовой пояс RFC3339")
	}

	to, err := time.Parse(time.RFC3339Nano, end)
	if err != nil {
		return Period{}, app.NewInvalidArgumentErr("Конец периода должен содержать дату, время и часовой пояс RFC3339")
	}

	return NewPeriod(from, to)
}

func (p Period) Validate() error {
	if p.start.IsZero() || p.end.IsZero() {
		return app.NewInvalidArgumentErr("Не указаны границы временного периода")
	}

	if !p.start.Before(p.end) {
		return app.NewInvalidArgumentErr("Конец периода должен быть позже начала")
	}

	// PostgreSQL хранит timestamptz с микросекундной точностью. Не округляем границы скрыто.
	if p.start.Nanosecond()%1000 != 0 || p.end.Nanosecond()%1000 != 0 {
		return app.NewInvalidArgumentErr("Границы периода допускают не более шести знаков долей секунды")
	}

	return nil
}

func (p Period) Start() time.Time {
	return p.start
}

func (p Period) End() time.Time {
	return p.end
}

func (p Period) Equal(other Period) bool {
	return p.start.Equal(other.start) && p.end.Equal(other.end)
}

func (p Period) Duration() time.Duration {
	return p.end.Sub(p.start)
}
