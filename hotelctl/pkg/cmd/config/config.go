// Package config exposes operator commands for service runtime configuration.
package config

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	projectconfig "github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/iostreams"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/rtconfig"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/pkg/cmd/util"
	"github.com/spf13/cobra"
)

type Options struct {
	util.Options
	Connection       rtconfig.Connection
	Environment      string
	Prefix           string
	ExpectedRevision int64
	CheckRevision    bool
	Service          projectconfig.Service
	Variable         rtconfig.Variable
	Key              string
	Value            string
	set              bool
	name             string
	argument         string
}

func NewCmdConfig(f util.Factory, streams iostreams.IOStreams) *cobra.Command {
	command := &cobra.Command{
		Use:   "config",
		Short: "Read and change service runtime values in etcd",
	}
	command.AddCommand(newCommand(f, streams, false), newCommand(f, streams, true))
	return command
}

func newCommand(f util.Factory, streams iostreams.IOStreams, set bool) *cobra.Command {
	o := &Options{
		Options: util.NewOptions(streams),
		set:     set,
	}
	o.Timeout = 5 * time.Second
	use, short, count := "get SERVICE VARIABLE", "Read a runtime override and its revision", 2
	if set {
		use, short, count = "set SERVICE VARIABLE VALUE", "Change a declared writable runtime variable", 3
	}
	command := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(count),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := o.Complete(f, args); err != nil {
				return err
			}
			o.CheckRevision = set && cmd.Flags().Changed("expect-revision")
			if err := o.Validate(); err != nil {
				return err
			}
			return o.Run(cmd.Context())
		},
	}
	if set {
		command.Example = "  hotelctl config set api-gateway hotels_list_enabled true\n  hotelctl config set api-gateway hotels_list_enabled false --expect-revision 42"
	} else {
		command.Example = "  hotelctl config get api-gateway hotels_list_enabled"
	}
	flags := command.Flags()
	o.AddFlags(flags)
	flags.StringSliceVar(&o.Connection.Endpoints, "endpoints", []string{"http://127.0.0.1:2379"}, "Comma-separated etcd endpoints reachable from this computer")
	flags.StringVar(&o.Environment, "env", "dev", "Environment used in the default etcd key prefix")
	flags.StringVar(&o.Prefix, "prefix", "", "Override the complete service prefix (absolute, ending in /)")
	flags.StringVar(&o.Connection.Username, "user", "", "Etcd operator username")
	flags.StringVar(&o.Connection.PasswordEnv, "password-env", "HOTELCTL_ETCD_PASSWORD", "Environment variable containing the etcd password")
	flags.StringVar(&o.Connection.CAFile, "cacert", "", "Trusted etcd CA PEM file")
	flags.StringVar(&o.Connection.CertFile, "cert", "", "Etcd client certificate PEM file")
	flags.StringVar(&o.Connection.KeyFile, "key", "", "Etcd client private key PEM file")
	if set {
		flags.Int64Var(&o.ExpectedRevision, "expect-revision", 0, "Require this key's ModRevision; explicit 0 means the key must not exist")
	}
	return command
}

func (o *Options) Complete(f util.Factory, args []string) error {
	if err := o.Options.Complete(f); err != nil {
		return err
	}
	services, err := o.Config.Select(args[:1])
	if err != nil {
		return err
	}
	o.Service = services[0]
	o.name = args[1]
	if o.set {
		o.argument = args[2]
	}
	return nil
}

var environmentName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,40}$`)

func (o *Options) Validate() error {
	if err := o.Options.Validate(); err != nil {
		return err
	}
	if err := o.Connection.Validate(); err != nil {
		return err
	}
	if !environmentName.MatchString(o.Environment) {
		return fmt.Errorf("--env must be a lowercase environment name")
	}
	if o.CheckRevision && o.ExpectedRevision < 0 {
		return fmt.Errorf("--expect-revision must not be negative")
	}
	prefix := o.Prefix
	if prefix == "" {
		prefix = fmt.Sprintf("/%s/%s/services/%s/rtconfig/v1/values/", o.Config.Name, o.Environment, o.Service.Name)
	}
	if !strings.HasPrefix(prefix, "/") || !strings.HasSuffix(prefix, "/") || prefix == "/" || strings.Contains(prefix, "//") {
		return fmt.Errorf("--prefix must be a non-root absolute path ending in /")
	}
	for _, component := range strings.Split(prefix, "/") {
		if component == "." || component == ".." {
			return fmt.Errorf("--prefix must not contain relative path components")
		}
	}
	filename := filepath.Join(o.Config.Root, o.Service.Context, "config", "rtconfig.yaml")
	variable, err := rtconfig.LoadVariable(filename, o.name)
	if err != nil {
		return fmt.Errorf("service %s: %w", o.Service.Name, err)
	}
	o.Variable = variable
	o.Key = prefix + variable.Name
	if !o.set {
		return nil
	}
	if !variable.Writable {
		return fmt.Errorf("service %s: runtime variable %q is readonly", o.Service.Name, variable.Name)
	}
	o.Value, err = variable.Encode(o.argument)
	return err
}

func (o *Options) Run(ctx context.Context) (resultErr error) {
	ctx, cancel := o.Context(ctx)
	defer cancel()
	// Runtime config is an independent control-plane connection. It does not
	// require a Docker client, a running stand, or its operation lock.
	client, err := rtconfig.Open(ctx, o.Connection)
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, client.Close())
	}()

	if o.set {
		if _, err := fmt.Fprintf(o.ErrOut, "etcd=%s key=%s\n", strings.Join(o.Connection.Endpoints, ","), o.Key); err != nil {
			return err
		}
		var expected *int64
		if o.CheckRevision {
			expected = &o.ExpectedRevision
		}
		value, err := client.Set(ctx, o.Key, o.Value, expected)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(o.Out, "%s.%s = %s (revision=%d)\n", o.Service.Name, o.Variable.Name, value.JSON, value.Revision)
		return err
	}

	value, err := client.Get(ctx, o.Key)
	if err != nil {
		return err
	}
	source := "etcd"
	if !value.Exists {
		value.JSON = o.Variable.Default
		source = "default"
	}
	_, err = fmt.Fprintf(o.Out, "%s.%s = %s (source=%s revision=%d key=%s)\n", o.Service.Name, o.Variable.Name, value.JSON, source, value.Revision, o.Key)
	return err
}
