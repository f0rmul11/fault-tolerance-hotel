package stand

import (
	"context"
	"fmt"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/kafkafault"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/kube"
)

func (m *Manager) KafkaFault(ctx context.Context, options kafkafault.Options) error {
	if err := options.Validate(); err != nil {
		return err
	}
	unlock, err := m.Store.Lock()
	if err != nil {
		return err
	}
	defer unlock()
	state, err := m.running(ctx)
	if err != nil {
		return err
	}
	if state.Reset != nil {
		return fmt.Errorf("сначала завершите hotelctl reset --yes")
	}
	client, err := kube.New(m.Config.Kubeconfig())
	if err != nil {
		return err
	}
	forward, err := client.ForwardKafkaProxy(ctx, m.Streams.ErrOut)
	if err != nil {
		return err
	}
	defer forward.Close()
	control := kafkafault.NewClient(fmt.Sprintf("http://127.0.0.1:%d", forward.Ports[8474]))
	defer control.Close()
	return kafkafault.Run(ctx, control, func(ctx context.Context) (map[int]int64, error) {
		return client.KafkaOffsets(ctx, options.Topic)
	}, options, m.Streams.Out)
}
