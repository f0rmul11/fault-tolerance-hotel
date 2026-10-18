package stop

import (
	"context"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/iostreams"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/pkg/cmd/util"
	"github.com/spf13/cobra"
)

type StopOptions struct {
	util.Options
	Components []config.Service
}

func NewStopOptions(streams iostreams.IOStreams) *StopOptions {
	return &StopOptions{
		Options: util.NewOptions(streams),
	}
}

func NewCmdStop(f util.Factory, streams iostreams.IOStreams) *cobra.Command {
	o := NewStopOptions(streams)
	command := &cobra.Command{
		Use:   "stop [COMPONENT...]",
		Short: "Остановить компоненты; без имён — весь k3s с сохранением данных",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := o.Complete(f, args); err != nil {
				return err
			}
			if err := o.Validate(); err != nil {
				return err
			}
			return o.Run(cmd.Context())
		},
	}
	o.AddFlags(command.Flags())
	return command
}

func (o *StopOptions) Complete(f util.Factory, args []string) error {
	if err := o.Options.Complete(f); err != nil {
		return err
	}
	var err error
	o.Components, err = o.Config.SelectComponents(args)
	return err
}

func (o *StopOptions) Validate() error {
	return o.Options.Validate()
}

func (o *StopOptions) Run(ctx context.Context) error {
	runner, err := o.NewStand()
	if err != nil {
		return err
	}
	defer runner.Close()
	ctx, cancel := o.Context(ctx)
	defer cancel()
	if len(o.Components) > 0 {
		return runner.Control(ctx, "stop", o.Components)
	}
	return runner.Stop(ctx)
}
