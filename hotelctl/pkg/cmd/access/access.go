package access

import (
	"fmt"
	"runtime"
	"text/tabwriter"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/iostreams"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/localaccess"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/pkg/cmd/util"
	"github.com/spf13/cobra"
)

func NewCmdAccess(f util.Factory, streams iostreams.IOStreams) *cobra.Command {
	return &cobra.Command{
		Use:   "access",
		Short: "Адреса API, Jaeger, Kafka UI, etcd и баз данных на компьютере",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			c, err := f.Config()
			if err != nil {
				return err
			}
			fmt.Fprintf(streams.Out, "HTTP: http://localhost:%d\nSwagger: http://localhost:%d/docs/\nKafka UI: http://localhost:8090\netcd: http://127.0.0.1:2379\n\n", c.Runtime.Ports.HTTP, c.Runtime.Ports.HTTP)
			if c.Observability.Enabled {
				fmt.Fprintln(streams.Out, "Jaeger: http://localhost:16686")
				fmt.Fprintln(streams.Out, "Grafana: http://localhost:3000 (логин admin; пароль в Secret hotelflow/grafana-admin)")
			}
			w := tabwriter.NewWriter(streams.Out, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "HOST\tPORT\tDATABASE\tUSER")
			for _, dependency := range config.Dependencies() {
				if dependency.Database != "" {
					fmt.Fprintf(w, "%s\t%d\t%s\t%s\n", dependency.Name, dependency.Port.Host, dependency.Database, dependency.Database)
				}
			}
			if err := w.Flush(); err != nil {
				return err
			}
			fmt.Fprintln(streams.Out, "\nПорты публикуются командой up; отдельные port-forward не нужны.\nИмена БД: sudo ./bin/hotelctl hosts --install (один раз).\nБез hosts используйте 127.0.0.1 и те же порты. SSL: disable.\nПароли учебных БД: deploy/booking-dependencies/credentials.yaml.")
			return nil
		},
	}
}

func NewCmdHosts(_ util.Factory, streams iostreams.IOStreams) *cobra.Command {
	var install, remove bool
	command := &cobra.Command{
		Use:   "hosts",
		Short: "Показать или установить локальные имена четырёх PostgreSQL",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if !install && !remove {
				fmt.Fprint(streams.Out, localaccess.HostsBlock())
				return nil
			}
			if runtime.GOOS == "windows" {
				return fmt.Errorf("на Windows добавьте вывод hotelctl hosts в системный hosts вручную")
			}
			if err := localaccess.UpdateHosts("/etc/hosts", remove); err != nil {
				return err
			}
			fmt.Fprintln(streams.Out, "OK /etc/hosts обновлён; остальные записи сохранены")
			return nil
		},
	}
	command.Flags().BoolVar(&install, "install", false, "Установить блок в /etc/hosts (требуется sudo)")
	command.Flags().BoolVar(&remove, "remove", false, "Удалить только блок hotelctl из /etc/hosts (требуется sudo)")
	command.MarkFlagsMutuallyExclusive("install", "remove")
	return command
}
