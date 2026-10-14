package destroy

import (
	"context"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/iostreams"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/pkg/cmd/util"
	"github.com/spf13/cobra"
)

type DestroyOptions struct {
	util.Options
}

func NewDestroyOptions(streams iostreams.IOStreams) *DestroyOptions {
	return &DestroyOptions{
		Options: util.NewOptions(streams),
	}
}

func NewCmdDestroy(f util.Factory, streams iostreams.IOStreams) *cobra.Command {
	o := NewDestroyOptions(streams)
	command := &cobra.Command{
		Use:   "destroy",
		Short: "Delete this stand and its data volume",
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

func (o *DestroyOptions) Complete(f util.Factory) error {
	return o.Options.Complete(f)
}

func (o *DestroyOptions) Validate() error {
	return o.Options.Validate()
}

func (o *DestroyOptions) Run(ctx context.Context) error {
	runner, err := o.NewStand()
	if err != nil {
		return err
	}
	defer runner.Close()
	ctx, cancel := o.Context(ctx)
	defer cancel()
	return runner.Destroy(ctx)
}
