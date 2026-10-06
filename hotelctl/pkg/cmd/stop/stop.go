package stop

import (
	"context"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/iostreams"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/pkg/cmd/util"
	"github.com/spf13/cobra"
)

type StopOptions struct{ util.Options }

func NewStopOptions(streams iostreams.IOStreams) *StopOptions {
	return &StopOptions{Options: util.NewOptions(streams)}
}

func NewCmdStop(f util.Factory, streams iostreams.IOStreams) *cobra.Command {
	o := NewStopOptions(streams)
	command := &cobra.Command{
		Use:   "stop",
		Short: "Stop k3s and preserve the data volume",
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
	return command
}

func (o *StopOptions) Complete(f util.Factory) error { return o.Options.Complete(f) }

func (o *StopOptions) Validate() error { return o.Options.Validate() }

func (o *StopOptions) Run(ctx context.Context) error {
	runner, err := o.NewStand()
	if err != nil {
		return err
	}
	defer runner.Close()
	ctx, cancel := o.Context(ctx)
	defer cancel()
	return runner.Stop(ctx)
}
