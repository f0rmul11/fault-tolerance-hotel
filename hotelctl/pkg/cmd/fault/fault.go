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
	Fault        faultctl.Options
	pushbackMS   int32
	percent      int
	delayProfile string
}

func NewCmdFault(f util.Factory, streams iostreams.IOStreams) *cobra.Command {
	o := &Options{
		Options: util.NewOptions(streams),
	}
	command := &cobra.Command{
		Use:   "fault FROM TO --slowdown 8s --duration 10s",
		Short: "Временно замедлить RPC или сделать связь недоступной через Envoy",
		Long: "Сбой на одной RPC-связи. --direction response задерживает готовый ответ.\n" +
			"--direction handler эмулирует задержку или UNAVAILABLE внутри Review.GetHotelRatings.\n" +
			"С --delay-per-inflight задержка растёт за каждый inflight сверх --inflight-threshold (по умолчанию 1).\n" +
			"--activate-on-enter держит базовую задержку до Enter; --delay-ramp плавно поднимает потолок задержки после активации.\n" +
			"--duration задаёт окно инъекции; уже задержанные вызовы могут закончиться позже.\n" +
			"Команда ждёт окончания окна и снимает правило; Ctrl+C снимает его досрочно.\n" +
			"После SIGKILL/сбоя CLI используйте --clear.",
		Example: "  hotelctl fault api-gateway booking --slowdown 8s --duration 10s\n" +
			"  hotelctl fault api-gateway booking --delay-profile '8:150ms,2:800ms' --duration 5m\n" +
			"  hotelctl fault api-gateway booking --direction response --slowdown 8s --duration 10s\n" +
			"  hotelctl fault hotels review --unavailable --duration 10s\n" +
			"  hotelctl fault hotels review --direction handler --slowdown 20ms --delay-per-inflight 20ms --max-delay 8s --duration 3m\n" +
			"  hotelctl fault api-gateway booking --unavailable --pushback-ms 1500 --duration 1m\n" +
			"  hotelctl fault api-gateway booking --unavailable --pushback-ms=-1 --duration 1m",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			o.Fault.From, o.Fault.To = args[0], args[1]
			if cmd.Flags().Changed("delay-profile") {
				profile, err := faultctl.ParseDelayProfile(o.delayProfile)
				if err != nil {
					return err
				}
				o.Fault.DelayProfile = profile
			}
			if cmd.Flags().Changed("percent") {
				o.Fault.Percent = &o.percent
			}
			if cmd.Flags().Changed("pushback-ms") {
				o.Fault.PushbackMS = &o.pushbackMS
			}
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
	command.Flags().StringVar(&o.delayProfile, "delay-profile", "", "Группы задержек api-gateway → booking: 8:150ms,2:800ms; остаток без задержки")
	command.Flags().IntVar(&o.percent, "percent", 100, "Доля физических попыток со сбоем, от 0 до 100 процентов")
	command.Flags().Int32Var(&o.pushbackMS, "pushback-ms", 0, "С --unavailable: gRPC pushback в мс; -1 запрещает retry, 0 разрешает без паузы")
	command.Flags().DurationVar(&o.Fault.Slowdown, "slowdown", 0, "Дополнительная задержка от 1ms до 1m")
	command.Flags().DurationVar(&o.Fault.DelayPerInflight, "delay-per-inflight", 0, "Для handler: добавка задержки за каждый другой допущенный запрос Review")
	command.Flags().DurationVar(&o.Fault.MaxDelay, "max-delay", 0, "Предел задержки с --delay-per-inflight, не больше 1m")
	command.Flags().IntVar(&o.Fault.InflightThreshold, "inflight-threshold", 0, "Число одновременных запросов Review, до которого задержка остаётся базовой (0: по умолчанию 1)")
	command.Flags().DurationVar(&o.Fault.DelayRamp, "delay-ramp", 0, "Время роста потолка задержки от --slowdown до --max-delay после активации")
	command.Flags().BoolVar(&o.Fault.ActivateOnEnter, "activate-on-enter", false, "Начать с базовой задержки и включить зависимость от inflight по Enter")
	command.Flags().DurationVar(&o.Fault.Duration, "duration", 0, "Окно действия сбоя от 1ms до 1h")
	command.Flags().StringVar(&o.Fault.Direction, "direction", "request", "Что задержать: request, response или handler (внутри Review)")
	command.Flags().BoolVar(&o.Fault.Unavailable, "unavailable", false, "Вернуть gRPC UNAVAILABLE; с --direction handler — внутри Review")
	command.Flags().BoolVar(&o.Fault.Clear, "clear", false, "Снять инъекции на выбранной связи после прерванного запуска")
	command.MarkFlagsMutuallyExclusive("slowdown", "unavailable")
	command.MarkFlagsMutuallyExclusive("clear", "slowdown", "unavailable")
	command.MarkFlagsMutuallyExclusive("clear", "pushback-ms")
	command.MarkFlagsMutuallyExclusive("clear", "percent")
	for _, flag := range []string{"slowdown", "percent", "unavailable", "pushback-ms", "clear"} {
		command.MarkFlagsMutuallyExclusive("delay-profile", flag)
	}
	command.MarkFlagsRequiredTogether("delay-per-inflight", "max-delay")
	for _, flag := range []string{"clear", "unavailable", "delay-profile", "pushback-ms"} {
		command.MarkFlagsMutuallyExclusive("delay-per-inflight", flag)
		command.MarkFlagsMutuallyExclusive("max-delay", flag)
	}
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
