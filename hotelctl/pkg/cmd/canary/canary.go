package canary

import (
	"context"
	"fmt"
	"strconv"
	"time"

	model "github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/canary"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/iostreams"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/pkg/cmd/util"
	"github.com/spf13/cobra"
)

func NewCmdCanary(f util.Factory, streams iostreams.IOStreams) *cobra.Command {
	root := &cobra.Command{Use: "canary", Short: "Канареечный релиз Review через Argo Rollouts и Istio"}
	for _, action := range []string{"setup", "deploy", "step", "status", "abort", "promote", "disable"} {
		o := util.NewOptions(streams)
		o.Timeout = 10 * time.Minute
		input := model.Options{Action: action}
		watch := false
		use := action + " review"
		args := cobra.ExactArgs(1)
		if action == "step" {
			use += " PERCENT"
			args = cobra.ExactArgs(2)
		}
		command := &cobra.Command{
			Use:   use,
			Short: descriptions[action],
			Args:  args,
			RunE: func(cmd *cobra.Command, args []string) error {
				input.Service = args[0]
				if input.Action == "step" {
					percent, err := strconv.Atoi(args[1])
					if err != nil {
						return fmt.Errorf("укажите целый процент: 10, 25, 50 или 100")
					}
					input.Percent = percent
				}
				if err := input.Validate(); err != nil {
					return err
				}
				if err := o.Complete(f); err != nil {
					return err
				}
				if err := o.Validate(); err != nil {
					return err
				}
				runner, err := o.NewStand()
				if err != nil {
					return err
				}
				defer runner.Close()
				for {
					ctx, cancel := o.Context(cmd.Context())
					err := runner.Canary(ctx, input)
					cancel()
					if err != nil {
						if watch && cmd.Context().Err() != nil {
							return nil
						}
						return err
					}
					if !watch {
						return nil
					}
					if err := wait(cmd.Context()); err != nil {
						return nil
					}
				}
			},
		}
		o.AddFlags(command.Flags())
		if action == "status" {
			command.Flags().BoolVarP(&watch, "watch", "w", false, "Обновлять состояние до Ctrl+C")
		}
		if action == "deploy" {
			command.Flags().BoolVar(&input.Restart, "restart", false, "Создать новую ревизию даже с тем же образом")
		}
		root.AddCommand(command)
	}
	return root
}

var descriptions = map[string]string{
	"setup":   "Установить контроллеры и перевести Hotels → Review в Istio",
	"deploy":  "Выкатить собранный образ Review и остановиться на 0%",
	"step":    "Перейти к следующему проценту: 10, 25, 50, 100",
	"status":  "Показать Rollout, ReplicaSet и веса Istio",
	"abort":   "Отменить релиз и вернуть трафик на stable",
	"promote": "Завершить релиз после ручной проверки на 100%",
	"disable": "Вернуть обычный Deployment и маршрут через Envoy",
}

func wait(ctx context.Context) error {
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
