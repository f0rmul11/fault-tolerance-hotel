package consumer

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/IBM/sarama"
)

type groupFake struct {
	sarama.ConsumerGroup
	calls atomic.Int32
}

func (g *groupFake) Consume(ctx context.Context, _ []string, _ sarama.ConsumerGroupHandler) error {
	g.calls.Add(1)

	<-ctx.Done()
	return ctx.Err()
}
func TestSubscribeStopRace(t *testing.T) {
	for range 100 {
		group := &groupFake{}
		consumer := NewTopicConsumer("events", group)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			consumer.Subscribe(context.Background(), nil)
		}()
		go func() {
			defer wg.Done()
			consumer.Stop()
		}()
		wg.Wait()
		consumer.Stop()
	}
}
func TestStoppedConsumerCannotStart(t *testing.T) {
	group := &groupFake{}
	consumer := NewTopicConsumer("events", group)
	consumer.Stop()
	consumer.Subscribe(context.Background(), nil)
	if consumer.cancel != nil || group.calls.Load() != 0 {
		t.Fatal("consumer started after shutdown")
	}
}
