package kafkafault

import (
	"time"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/iostreams"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/kafkafault"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/pkg/cmd/util"
	"github.com/spf13/cobra"
)

func NewCommand(f util.Factory, streams iostreams.IOStreams) *cobra.Command {
	o := util.NewOptions(streams)
	options := kafkafault.Options{}
	command := &cobra.Command{
		Use:   "fault-kafka",
		Short: "Разорвать TCP producer после записи события, удержав ответ Kafka",
		Long: "Только Booking producer. Предварительно создайте бронь, затем после ACTIVE отмените её.\n" +
			"Чтение consumer, AKHQ и репликация идут напрямую. Ctrl+C снимает правило; после SIGKILL используйте --clear.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := o.Complete(f); err != nil {
				return err
			}
			if err := o.Validate(); err != nil {
				return err
			}
			if err := options.Validate(); err != nil {
				return err
			}
			runner, err := o.NewStand()
			if err != nil {
				return err
			}
			defer runner.Close()
			ctx, cancel := o.Context(cmd.Context())
			defer cancel()
			return runner.KafkaFault(ctx, options)
		},
	}
	o.AddFlags(command.Flags())
	command.Flags().StringVar(&options.Topic, "topic", "booking.events.v1", "Топик, в котором ожидаем новую запись")
	command.Flags().DurationVar(&options.Duration, "duration", 200*time.Millisecond, "Продолжительность разрыва после записи, от 1ms до 1s")
	command.Flags().BoolVar(&options.Clear, "clear", false, "Восстановить три соединения и снять инъекцию")
	return command
}
