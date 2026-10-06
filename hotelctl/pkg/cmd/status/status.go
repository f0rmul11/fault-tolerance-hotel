package status

import (
	"context"
	"fmt"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/iostreams"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/pkg/cmd/util"
	"github.com/spf13/cobra"
)

type StatusOptions struct {
	util.Options
	Output string
}

func NewStatusOptions(streams iostreams.IOStreams) *StatusOptions {
	return &StatusOptions{Options: util.NewOptions(streams), Output: "table"}
}

func NewCmdStatus(f util.Factory, streams iostreams.IOStreams) *cobra.Command {
	o := NewStatusOptions(streams)
	command := &cobra.Command{
		Use:   "status",
		Short: "Show the node and service status",
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
	command.Flags().StringVarP(&o.Output, "output", "o", o.Output, "Output format: table or json")
	return command
}

func (o *StatusOptions) Complete(f util.Factory) error { return o.Options.Complete(f) }

func (o *StatusOptions) Validate() error {
	if o.Output != "table" && o.Output != "json" {
		return fmt.Errorf("--output must be table or json")
	}
	return o.Options.Validate()
}

func (o *StatusOptions) Run(ctx context.Context) error {
	runner, err := o.NewStand()
	if err != nil {
		return err
	}
	defer runner.Close()
	ctx, cancel := o.Context(ctx)
	defer cancel()
	return runner.Status(ctx, o.Output)
}
