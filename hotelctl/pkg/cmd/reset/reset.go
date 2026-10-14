package reset

import (
	"context"
	"fmt"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/iostreams"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/pkg/cmd/util"
	"github.com/spf13/cobra"
)

type ResetOptions struct {
	util.Options
	Yes bool
}

func NewCmdReset(f util.Factory, streams iostreams.IOStreams) *cobra.Command {
	o := &ResetOptions{
		Options: util.NewOptions(streams),
	}
	command := &cobra.Command{
		Use:   "reset",
		Short: "Сбросить учебные данные четырёх БД и Kafka, восстановить seed",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
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
	command.Flags().BoolVar(&o.Yes, "yes", false, "Удалить учебные записи, события и offsets без интерактивного вопроса")
	return command
}

func (o *ResetOptions) Validate() error {
	if err := o.Options.Validate(); err != nil {
		return err
	}
	if !o.Yes {
		return fmt.Errorf("reset удаляет учебные данные четырёх БД, два топика и offsets потребителей; используйте hotelctl reset --yes")
	}
	return nil
}

func (o *ResetOptions) Run(ctx context.Context) error {
	runner, err := o.NewStand()
	if err != nil {
		return err
	}
	defer runner.Close()
	ctx, cancel := o.Context(ctx)
	defer cancel()
	return runner.Reset(ctx)
}
