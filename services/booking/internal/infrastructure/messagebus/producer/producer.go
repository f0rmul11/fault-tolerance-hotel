package producer

import (
	"booking/internal/pkg/msgbus/producer"
)

type Producers struct {
	BookingEvents *producer.MessageProducer
}

func NewProducers() Producers {
	return Producers{
		BookingEvents: producer.NewMessageProducer(),
	}
}
