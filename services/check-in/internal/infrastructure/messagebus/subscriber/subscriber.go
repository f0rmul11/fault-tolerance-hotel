package subscriber

import (
	"check-in/config"
	"check-in/internal/pkg/msgbus/subscriber"
)

type Subscribers struct{ BookingEvents *subscriber.MessageSubscriber }

func NewSubscribers() Subscribers {
	return Subscribers{BookingEvents: subscriber.NewMessageSubscriber(config.BookingEventsTopic)}
}
