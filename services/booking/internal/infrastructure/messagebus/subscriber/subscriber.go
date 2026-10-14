package subscriber

import (
	"booking/config"
	"booking/internal/pkg/msgbus/subscriber"
)

type Subscribers struct{ CheckinEvents *subscriber.MessageSubscriber }

func NewSubscribers() Subscribers {
	return Subscribers{CheckinEvents: subscriber.NewMessageSubscriber(config.CheckinEventsTopic)}
}
