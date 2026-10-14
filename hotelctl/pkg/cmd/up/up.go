package up

import (
	"context"
	"time"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/iostreams"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/pkg/cmd/util"
	"github.com/spf13/cobra"
)

type UpOptions struct {
	util.Options
	Services []config.Service
}

func NewUpOptions(streams iostreams.IOStreams) *UpOptions {
	options := &UpOptions{
		Options: util.NewOptions(streams),
	}
	options.Timeout = 15 * time.Minute
	return options
}

func NewCmdUp(f util.Factory, streams iostreams.IOStreams) *cobra.Command {
	o := NewUpOptions(streams)
	command := &cobra.Command{
		Use:     "up [SERVICE...]",
		Short:   "Start one k3s node, build, deploy and check the stand",
		Example: "  hotelctl up\n  hotelctl up api-gateway",
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

func (o *UpOptions) Complete(f util.Factory, args []string) error {
	if err := o.Options.Complete(f); err != nil {
		return err
	}
	var err error
	o.Services, err = o.Config.Select(args)
	return err
}

func (o *UpOptions) Validate() error {
	return o.Options.Validate()
}

func (o *UpOptions) Run(ctx context.Context) error {
	runner, err := o.NewStand()
	if err != nil {
		return err
	}
	defer runner.Close()
	ctx, cancel := o.Context(ctx)
	defer cancel()
	return runner.Up(ctx, o.Services)
}
