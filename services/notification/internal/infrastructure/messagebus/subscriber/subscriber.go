package subscriber

import (
	"notification/config"
	"notification/internal/pkg/msgbus/subscriber"
)

type Subscribers struct {
	BookingEvents *subscriber.MessageSubscriber
}

func NewSubscribers() Subscribers {
	return Subscribers{
		BookingEvents: subscriber.NewMessageSubscriber(config.BookingEventsTopic),
	}
}
