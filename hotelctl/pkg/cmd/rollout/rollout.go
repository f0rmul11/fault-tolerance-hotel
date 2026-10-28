package rollout

import (
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/iostreams"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/pkg/cmd/util"
	"github.com/spf13/cobra"
)

func NewCmdRollout(f util.Factory, streams iostreams.IOStreams) *cobra.Command {
	root := &cobra.Command{Use: "rollout", Short: "Управление обновлением Deployment"}
	o := util.NewOptions(streams)
	cmd := &cobra.Command{Use: "restart SERVICE", Short: "Запустить замену Pod согласно стратегии Deployment", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := o.Complete(f); err != nil {
			return err
		}
		if err := o.Validate(); err != nil {
			return err
		}
		services, err := o.Config.Select(args)
		if err != nil {
			return err
		}
		runner, err := o.NewStand()
		if err != nil {
			return err
		}
		defer runner.Close()
		ctx, cancel := o.Context(cmd.Context())
		defer cancel()
		return runner.RolloutRestart(ctx, services[0])
	}}
	o.AddFlags(cmd.Flags())
	root.AddCommand(cmd)
	return root
}
