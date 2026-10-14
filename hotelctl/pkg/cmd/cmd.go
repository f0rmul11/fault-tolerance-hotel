package cmd

import (
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/iostreams"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/pkg/cmd/build"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/pkg/cmd/deploy"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/pkg/cmd/destroy"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/pkg/cmd/doctor"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/pkg/cmd/kubectl"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/pkg/cmd/logs"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/pkg/cmd/reset"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/pkg/cmd/smoke"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/pkg/cmd/status"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/pkg/cmd/stop"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/pkg/cmd/up"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/pkg/cmd/util"
	"github.com/spf13/cobra"
)

func NewDefaultHotelctlCommand(streams iostreams.IOStreams, version string) *cobra.Command {
	flags := &util.ConfigFlags{}
	command := NewHotelctlCommand(util.NewFactory(flags), streams, version)
	flags.AddFlags(command.PersistentFlags())
	return command
}

// NewHotelctlCommand constructs the command tree without opening network connections.
func NewHotelctlCommand(f util.Factory, streams iostreams.IOStreams, version string) *cobra.Command {
	command := &cobra.Command{
		Use:           "hotelctl",
		Short:         "Develop and run the HotelFlow course stand",
		Long:          "HotelFlow: one k3s node, service builds, deployments and diagnostics.\nConfiguration: hotel.yaml in the project root.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	command.SetIn(streams.In)
	command.SetOut(streams.Out)
	command.SetErr(streams.ErrOut)
	command.AddCommand(up.NewCmdUp(f, streams))
	command.AddCommand(build.NewCmdBuild(f, streams))
	command.AddCommand(deploy.NewCmdDeploy(f, streams))
	command.AddCommand(stop.NewCmdStop(f, streams))
	command.AddCommand(destroy.NewCmdDestroy(f, streams))
	command.AddCommand(smoke.NewCmdSmoke(f, streams))
	command.AddCommand(reset.NewCmdReset(f, streams))
	command.AddCommand(doctor.NewCmdDoctor(f, streams))
	command.AddCommand(status.NewCmdStatus(f, streams))
	command.AddCommand(logs.NewCmdLogs(f, streams))
	command.AddCommand(kubectl.NewCmdKubectl(f, streams))
	return command
}
