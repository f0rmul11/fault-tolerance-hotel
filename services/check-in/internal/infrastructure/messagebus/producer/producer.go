package producer

import (
	"check-in/internal/pkg/msgbus/producer"
)

type Producers struct {
	CheckinEvents *producer.MessageProducer
}

func NewProducers() Producers {
	return Producers{
		CheckinEvents: producer.NewMessageProducer(),
	}
}
