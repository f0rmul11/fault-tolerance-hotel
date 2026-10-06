package kubectl

import (
	"context"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/iostreams"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/pkg/cmd/util"
	"github.com/spf13/cobra"
)

type KubectlOptions struct {
	util.Options
	Args []string
}

func NewKubectlOptions(streams iostreams.IOStreams) *KubectlOptions {
	return &KubectlOptions{Options: util.NewOptions(streams)}
}

func NewCmdKubectl(f util.Factory, streams iostreams.IOStreams) *cobra.Command {
	o := NewKubectlOptions(streams)
	return &cobra.Command{
		Use:                "kubectl [ARGS...]",
		Short:              "Run the kubectl bundled with k3s (no local install required)",
		DisableFlagParsing: true,
		Example:            "  hotelctl kubectl get nodes\n  hotelctl kubectl -n hotelflow get pods",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 || (len(args) == 1 && (args[0] == "--help" || args[0] == "-h")) {
				return cmd.Help()
			}
			if err := o.Complete(f, args); err != nil {
				return err
			}
			if err := o.Validate(); err != nil {
				return err
			}
			return o.Run(cmd.Context())
		},
	}
}

func (o *KubectlOptions) Complete(f util.Factory, args []string) error {
	o.Args = args
	return o.Options.Complete(f)
}

func (o *KubectlOptions) Validate() error { return nil }

func (o *KubectlOptions) Run(ctx context.Context) error {
	runner, err := o.NewStand()
	if err != nil {
		return err
	}
	defer runner.Close()
	return runner.Kubectl(ctx, o.Args)
}
