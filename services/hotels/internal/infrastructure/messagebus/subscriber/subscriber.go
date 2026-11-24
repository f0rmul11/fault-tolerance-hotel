package subscriber

import (
	"hotels/config"
	"hotels/internal/pkg/msgbus/subscriber"
)

type Subscribers struct {
	ReviewEvents *subscriber.MessageSubscriber
}

func NewSubscribers() Subscribers {
	return Subscribers{
		ReviewEvents: subscriber.NewMessageSubscriber(config.ReviewEventsTopic),
	}
}
