package message

import "booking/internal/pkg/event"

// Message содержит готовые к отправке данные одной записи Kafka.
type Message struct {
	Topic       string
	PartitionID int32
	Key         []byte
	Body        []byte
	Headers     []byte
}

func FromEvent(ev event.Event, partitionID int32) Message {
	return Message{
		Topic:       ev.Schema,
		PartitionID: partitionID,
		Key:         ev.Key,
		Body:        ev.Body,
		Headers:     ev.Headers,
	}
}
