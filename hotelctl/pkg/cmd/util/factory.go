package util

import (
	"context"
	"fmt"
	"time"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/docker"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/iostreams"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/stand"
	"github.com/spf13/pflag"
)

type Factory interface {
	Config() (config.Config, error)
	NewStand(config.Config, iostreams.IOStreams) (stand.Interface, error)
}

type ConfigFlags struct {
	Project    string
	ConfigFile string
}

func (f *ConfigFlags) AddFlags(flags *pflag.FlagSet) {
	flags.StringVar(
		&f.Project,
		"project",
		"",
		"Project directory (defaults to the closest parent with hotel.yaml)",
	)
	flags.StringVar(&f.ConfigFile, "config", "", "Path to an alternative hotel.yaml")
}

type factory struct {
	flags *ConfigFlags
}

func NewFactory(flags *ConfigFlags) Factory {
	return &factory{
		flags: flags,
	}
}

func (f *factory) Config() (config.Config, error) {
	return config.Load(f.flags.Project, f.flags.ConfigFile)
}

func (f *factory) NewStand(c config.Config, s iostreams.IOStreams) (stand.Interface, error) {
	engine, err := docker.New(s.ErrOut)
	if err != nil {
		return nil, err
	}
	return stand.New(c, engine, s), nil
}

// Options are shared inputs, not shared command execution or mutable globals.
type Options struct {
	Config  config.Config
	Timeout time.Duration
	Factory Factory
	iostreams.IOStreams
}

func NewOptions(streams iostreams.IOStreams) Options {
	return Options{
		Timeout:   5 * time.Minute,
		IOStreams: streams,
	}
}

func (o *Options) Complete(f Factory) error {
	c, err := f.Config()
	if err != nil {
		return err
	}
	o.Config, o.Factory = c, f
	return nil
}

func (o *Options) Validate() error {
	if o.Timeout <= 0 {
		return fmt.Errorf("--timeout must be positive")
	}
	return nil
}

func (o *Options) AddFlags(flags *pflag.FlagSet) {
	flags.DurationVar(&o.Timeout, "timeout", o.Timeout, "Maximum time for this operation (for example 30s or 5m)")
}

func (o *Options) Context(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, o.Timeout)
}

func (o *Options) NewStand() (stand.Interface, error) {
	return o.Factory.NewStand(o.Config, o.IOStreams)
}
