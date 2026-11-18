package kafkafault

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

type Options struct {
	Topic    string
	Duration time.Duration
	Clear    bool
}

func (o Options) Validate() error {
	if o.Clear {
		return nil
	}
	if o.Topic != "booking.events.v1" {
		return fmt.Errorf("потеря ответа поддерживается для booking.events.v1")
	}
	if o.Duration < time.Millisecond || o.Duration > time.Second {
		return fmt.Errorf("разрыв должен длиться от 1ms до 1s, чтобы повтор остался внутри producer")
	}
	return nil
}

type Control interface {
	BlockResponses(context.Context) error
	Disconnect(context.Context) error
	Restore(context.Context) error
}

type Offsets func(context.Context) (map[int]int64, error)

// Run ждёт реально появившуюся запись и только затем закрывает TCP.
// ACK удерживается downstream toxic; приложение и брокер не подменяются.
func Run(ctx context.Context, control Control, offsets Offsets, o Options, out io.Writer) (err error) {
	if err := o.Validate(); err != nil {
		return err
	}
	if o.Clear {
		if err := control.Restore(ctx); err != nil {
			return err
		}
		fmt.Fprintln(out, "OK связь Booking producer → Kafka восстановлена")
		return nil
	}
	before, err := offsets(ctx)
	if err != nil {
		return err
	}
	if len(before) == 0 {
		return fmt.Errorf("в топике нет доступных партиций")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		if restoreErr := control.Restore(cleanup); restoreErr != nil {
			err = errors.Join(err, restoreErr)
		} else {
			fmt.Fprintln(out, "OK связь восстановлена, правило снято")
		}
	}()
	if err := control.BlockResponses(ctx); err != nil {
		return err
	}
	fmt.Fprintln(out, "ACTIVE: ответы producer удерживаются. Выполните отмену предварительно созданной брони.")
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	for {
		after, err := offsets(ctx)
		if err != nil {
			return err
		}
		if len(before) != len(after) {
			return fmt.Errorf("число партиций изменилось во время опыта")
		}
		for partition, end := range after {
			start, exists := before[partition]
			if !exists || end < start {
				return fmt.Errorf("топик был изменён или сброшен во время опыта")
			}
			if end > start {
				fmt.Fprintf(out, "APPEND: %s P%d end %d → %d. Закрываем TCP до получения ответа producer.\n", o.Topic, partition, start, end)
				if err := control.Disconnect(ctx); err != nil {
					return err
				}
				timer := time.NewTimer(o.Duration)
				defer timer.Stop()
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-timer.C:
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("новая запись не появилась: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
