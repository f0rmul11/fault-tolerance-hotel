package rtconfig_test

import (
	"context"
	"fmt"

	config "hotels/internal/config"
	"hotels/internal/pkg/rtconfig"
)

func ExampleNew() {
	schema, err := rtconfig.NewSchema(rtconfig.Definition{
		Name:     "rooms_list_enabled",
		Type:     rtconfig.BoolType,
		Default:  false,
		Writable: true,
	})
	if err != nil {
		panic(err)
	}
	manager, err := rtconfig.New(schema, rtconfig.Options{
		Endpoints: []string{"http://127.0.0.1:2379"},
		Prefix:    "/hotelflow/dev/services/hotels/rtconfig/v1/values/",
	})
	if err != nil {
		panic(err)
	}

	// Defaults are available before Start, without contacting etcd.
	cfg := config.NewClient(manager)
	fmt.Println(cfg.GetValue(context.Background(), config.RoomsListEnabled).Bool())
	if err := manager.Close(context.Background()); err != nil {
		panic(err)
	}
	// Output: false
}
