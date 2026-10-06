package doctor

import (
	"context"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/iostreams"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/pkg/cmd/util"
	"github.com/spf13/cobra"
)

type DoctorOptions struct{ util.Options }

func NewDoctorOptions(streams iostreams.IOStreams) *DoctorOptions {
	return &DoctorOptions{Options: util.NewOptions(streams)}
}

func NewCmdDoctor(f util.Factory, streams iostreams.IOStreams) *cobra.Command {
	o := NewDoctorOptions(streams)
	command := &cobra.Command{
		Use:   "doctor",
		Short: "Check configuration, Docker, host ports and k3s readiness",
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

func (o *DoctorOptions) Complete(f util.Factory) error { return o.Options.Complete(f) }

func (o *DoctorOptions) Validate() error { return o.Options.Validate() }

func (o *DoctorOptions) Run(ctx context.Context) error {
	runner, err := o.NewStand()
	if err != nil {
		return err
	}
	defer runner.Close()
	ctx, cancel := o.Context(ctx)
	defer cancel()
	return runner.Doctor(ctx)
}
