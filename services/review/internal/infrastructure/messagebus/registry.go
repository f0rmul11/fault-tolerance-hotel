package messagebus

import (
	"review/internal/pkg/closer"
	"review/internal/pkg/msgbus/producer"
)

type Registry struct {
	RatingEvents *producer.MessageProducer
}

func NewRegistry() (*Registry, error) {
	publisher, err := producer.NewMessageProducer()
	if err != nil {
		return nil, err
	}
	closer.Add(publisher.Close)
	return &Registry{RatingEvents: publisher}, nil
}
