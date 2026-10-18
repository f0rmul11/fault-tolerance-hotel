package logs

import (
	"context"
	"fmt"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/iostreams"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/pkg/cmd/util"
	"github.com/spf13/cobra"
)

type LogsOptions struct {
	util.Options
	Service config.Service
	Follow  bool
	Tail    int64
}

func NewLogsOptions(streams iostreams.IOStreams) *LogsOptions {
	return &LogsOptions{
		Options: util.NewOptions(streams),
		Tail:    100,
	}
}

func NewCmdLogs(f util.Factory, streams iostreams.IOStreams) *cobra.Command {
	o := NewLogsOptions(streams)
	command := &cobra.Command{
		Use:     "logs COMPONENT",
		Short:   "Показать логи приложения или зависимости",
		Args:    cobra.ExactArgs(1),
		Example: "  hotelctl logs api-gateway -f --tail 50",
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
	command.Flags().BoolVarP(&o.Follow, "follow", "f", false, "Follow logs until interrupted")
	command.Flags().Int64Var(&o.Tail, "tail", 100, "Number of recent log lines")
	return command
}

func (o *LogsOptions) Complete(f util.Factory, args []string) error {
	if err := o.Options.Complete(f); err != nil {
		return err
	}
	services, err := o.Config.SelectComponents(args)
	if err != nil {
		return err
	}
	o.Service = services[0]
	return nil
}

func (o *LogsOptions) Validate() error {
	if o.Tail < 0 {
		return fmt.Errorf("--tail must not be negative")
	}
	return o.Options.Validate()
}

func (o *LogsOptions) Run(ctx context.Context) error {
	runner, err := o.NewStand()
	if err != nil {
		return err
	}
	defer runner.Close()
	if !o.Follow {
		var cancel context.CancelFunc
		ctx, cancel = o.Context(ctx)
		defer cancel()
	}
	return runner.Logs(ctx, o.Service, o.Follow, o.Tail)
}
