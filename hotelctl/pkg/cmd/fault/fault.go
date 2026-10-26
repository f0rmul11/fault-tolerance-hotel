package fault

import (
	"context"
	"fmt"
	"time"

	faultctl "github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/fault"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/iostreams"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/pkg/cmd/util"
	"github.com/spf13/cobra"
)

type Options struct {
	util.Options
	Fault faultctl.Options
}

func NewCmdFault(f util.Factory, streams iostreams.IOStreams) *cobra.Command {
	o := &Options{
		Options: util.NewOptions(streams),
	}
	command := &cobra.Command{
		Use:   "fault FROM TO --slowdown 8s --duration 10s",
		Short: "Временно замедлить RPC или сделать связь недоступной через Envoy",
		Long: "Сбой на одной RPC-связи. --direction response задерживает готовый ответ.\n" +
			"--duration задаёт окно инъекции; уже задержанные вызовы могут закончиться позже.\n" +
			"Команда ждёт окончания окна и снимает правило; Ctrl+C снимает его досрочно.\n" +
			"После SIGKILL/сбоя CLI используйте --clear.",
		Example: "  hotelctl fault api-gateway booking --slowdown 8s --duration 10s\n" +
			"  hotelctl fault api-gateway booking --direction response --slowdown 8s --duration 10s\n" +
			"  hotelctl fault hotels review --unavailable --duration 10s",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			o.Fault.From, o.Fault.To = args[0], args[1]
			if err := o.Complete(f); err != nil {
				return err
			}
			if err := o.Validate(); err != nil {
				return err
			}
			return o.Run(cmd.Context())
		},
	}
	o.AddFlags(command.Flags())
	command.Flags().DurationVar(&o.Fault.Slowdown, "slowdown", 0, "Дополнительная задержка от 1ms до 1m")
	command.Flags().DurationVar(&o.Fault.Duration, "duration", 0, "Окно действия сбоя от 1ms до 1h")
	command.Flags().StringVar(&o.Fault.Direction, "direction", "request", "Что задержать: request или response")
	command.Flags().BoolVar(&o.Fault.Unavailable, "unavailable", false, "Вернуть gRPC UNAVAILABLE без вызова downstream")
	command.Flags().BoolVar(&o.Fault.Clear, "clear", false, "Снять инъекции на выбранной связи после прерванного запуска")
	command.MarkFlagsMutuallyExclusive("slowdown", "unavailable")
	command.MarkFlagsMutuallyExclusive("clear", "slowdown", "unavailable")
	return command
}

func (o *Options) Validate() error {
	if err := o.Options.Validate(); err != nil {
		return err
	}
	if err := o.Fault.Validate(); err != nil {
		return err
	}
	if _, err := o.Config.Select([]string{o.Fault.From, o.Fault.To}); err != nil {
		return err
	}
	if o.Timeout < o.Fault.Duration+15*time.Second {
		return fmt.Errorf("--timeout должен превышать --duration минимум на 15s для подключения и проверки восстановления")
	}
	return nil
}

func (o *Options) Run(ctx context.Context) error {
	runner, err := o.NewStand()
	if err != nil {
		return err
	}
	defer runner.Close()
	ctx, cancel := o.Context(ctx)
	defer cancel()
	return runner.Fault(ctx, o.Fault)
}
