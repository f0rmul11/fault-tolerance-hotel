package deploy

import (
	"context"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/iostreams"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/pkg/cmd/util"
	"github.com/spf13/cobra"
)

type DeployOptions struct {
	util.Options
	Services []config.Service
}

func NewDeployOptions(streams iostreams.IOStreams) *DeployOptions {
	return &DeployOptions{Options: util.NewOptions(streams)}
}

func NewCmdDeploy(f util.Factory, streams iostreams.IOStreams) *cobra.Command {
	o := NewDeployOptions(streams)
	command := &cobra.Command{
		Use:     "deploy [SERVICE...]",
		Short:   "Deploy previously built service images",
		Example: "  hotelctl deploy\n  hotelctl deploy api-gateway",
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

func (o *DeployOptions) Complete(f util.Factory, args []string) error {
	if err := o.Options.Complete(f); err != nil {
		return err
	}
	var err error
	o.Services, err = o.Config.Select(args)
	return err
}

func (o *DeployOptions) Validate() error { return o.Options.Validate() }

func (o *DeployOptions) Run(ctx context.Context) error {
	runner, err := o.NewStand()
	if err != nil {
		return err
	}
	defer runner.Close()
	ctx, cancel := o.Context(ctx)
	defer cancel()
	return runner.Deploy(ctx, o.Services)
}
