package status

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/iostreams"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/pkg/cmd/util"
	"github.com/spf13/cobra"
)

type StatusOptions struct {
	util.Options
	Output  string
	Watch   bool
	Service *config.Service
}

func NewStatusOptions(streams iostreams.IOStreams) *StatusOptions {
	return &StatusOptions{
		Options: util.NewOptions(streams),
		Output:  "table",
	}
}

func NewCmdStatus(f util.Factory, streams iostreams.IOStreams) *cobra.Command {
	o := NewStatusOptions(streams)
	command := &cobra.Command{
		Use:   "status [SERVICE]",
		Short: "Show the node and service status",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := o.Complete(f); err != nil {
				return err
			}
			if err := o.Validate(); err != nil {
				return err
			}
			if len(args) == 1 {
				services, err := o.Config.Select(args)
				if err != nil {
					return err
				}
				o.Service = &services[0]
			}
			return o.Run(cmd.Context())
		},
	}
	o.AddFlags(command.Flags())
	command.Flags().BoolVarP(&o.Watch, "watch", "w", false, "Обновлять состояние каждую секунду до Ctrl+C")
	command.Flags().StringVarP(&o.Output, "output", "o", o.Output, "Output format: table or json")
	return command
}

func (o *StatusOptions) Complete(f util.Factory) error {
	return o.Options.Complete(f)
}

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
	if !o.Watch {
		var cancel context.CancelFunc
		ctx, cancel = o.Context(ctx)
		defer cancel()
	}
	for {
		if o.Watch && o.Output == "table" {
			if file, ok := o.Out.(*os.File); ok {
				if info, err := file.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
					fmt.Fprint(o.Out, "\033[H\033[2J")
				}
			}
		}
		var err error
		if o.Service != nil {
			err = runner.PodStatus(ctx, *o.Service, o.Output)
		} else {
			err = runner.Status(ctx, o.Output)
		}
		if ctx.Err() != nil && o.Watch {
			return nil
		}
		if err != nil {
			if !o.Watch {
				return err
			}
			fmt.Fprintf(o.ErrOut, "Не удалось обновить состояние: %v\n", err)
		}
		if !o.Watch {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Second):
		}
	}
}
