package build

import (
	"context"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/iostreams"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/pkg/cmd/util"
	"github.com/spf13/cobra"
)

type BuildOptions struct {
	util.Options
	Services []config.Service
}

func NewBuildOptions(streams iostreams.IOStreams) *BuildOptions {
	return &BuildOptions{Options: util.NewOptions(streams)}
}

func NewCmdBuild(f util.Factory, streams iostreams.IOStreams) *cobra.Command {
	o := NewBuildOptions(streams)
	command := &cobra.Command{
		Use:     "build [SERVICE...]",
		Short:   "Build local images for selected services",
		Example: "  hotelctl build\n  hotelctl build api-gateway",
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

func (o *BuildOptions) Complete(f util.Factory, args []string) error {
	if err := o.Options.Complete(f); err != nil {
		return err
	}
	var err error
	o.Services, err = o.Config.Select(args)
	return err
}

func (o *BuildOptions) Validate() error { return o.Options.Validate() }

func (o *BuildOptions) Run(ctx context.Context) error {
	runner, err := o.NewStand()
	if err != nil {
		return err
	}
	defer runner.Close()
	ctx, cancel := o.Context(ctx)
	defer cancel()
	return runner.Build(ctx, o.Services)
}
