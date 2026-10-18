package component

import (
	"context"
	"fmt"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/iostreams"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/pkg/cmd/util"
	"github.com/spf13/cobra"
)

type Options struct {
	util.Options
	Action     string
	Components []config.Service
}

func NewCmdControl(f util.Factory, streams iostreams.IOStreams, action string) *cobra.Command {
	o := &Options{
		Options: util.NewOptions(streams),
		Action:  action,
	}
	command := &cobra.Command{
		Use: action + " COMPONENT [COMPONENT...]",
		Short: map[string]string{
			"start":   "Запустить остановленные компоненты без пересборки",
			"restart": "Остановить и снова запустить выбранные компоненты",
		}[action],
		Args: cobra.MinimumNArgs(1),
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

func (o *Options) Complete(f util.Factory, args []string) error {
	if err := o.Options.Complete(f); err != nil {
		return err
	}
	var err error
	o.Components, err = o.Config.SelectComponents(args)
	return err
}

func (o *Options) Validate() error {
	return o.Options.Validate()
}

func (o *Options) Run(ctx context.Context) error {
	runner, err := o.NewStand()
	if err != nil {
		return err
	}
	defer runner.Close()
	ctx, cancel := o.Context(ctx)
	defer cancel()
	return runner.Control(ctx, o.Action, o.Components)
}

func NewCmdComponents(f util.Factory, streams iostreams.IOStreams) *cobra.Command {
	return &cobra.Command{
		Use:   "components",
		Short: "Имена компонентов для stop/start/restart/logs",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			c, err := f.Config()
			if err != nil {
				return err
			}
			for _, component := range c.Components() {
				fmt.Fprintln(streams.Out, component.Name)
			}
			return nil
		},
	}
}
