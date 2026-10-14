package consumer

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/IBM/sarama"
)

type sessionFake struct {
	sarama.ConsumerGroupSession
	steps []string
}

func (s *sessionFake) Context() context.Context {
	return context.Background()
}

func (s *sessionFake) MarkMessage(_ *sarama.ConsumerMessage, _ string) {
	s.steps = append(s.steps, "mark")
}

func (s *sessionFake) Commit() {
	s.steps = append(s.steps, "commit")
}

type claimFake struct {
	sarama.ConsumerGroupClaim
	messages chan *sarama.ConsumerMessage
}

func (c claimFake) Messages() <-chan *sarama.ConsumerMessage {
	return c.messages
}

func TestCommitOnlyAfterSuccessfulSend(t *testing.T) {
	failure := errors.New("провайдер недоступен")
	for _, test := range []struct {
		name  string
		err   error
		steps []string
	}{
		{
			name:  "sent",
			steps: []string{"send", "mark", "commit"},
		},
		{
			name:  "failed",
			err:   failure,
			steps: []string{"send"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := &sessionFake{}
			messages := make(chan *sarama.ConsumerMessage, 1)
			messages <- &sarama.ConsumerMessage{}
			close(messages)
			consumer := groupSubscriber{
				messageHandler: func(context.Context, sarama.ConsumerGroupSession, *sarama.ConsumerMessage) error {
					session.steps = append(session.steps, "send")
					return test.err
				},
			}
			err := consumer.ConsumeClaim(session, claimFake{messages: messages})
			if !errors.Is(err, test.err) || !reflect.DeepEqual(session.steps, test.steps) {
				t.Fatalf("steps=%v, error=%v", session.steps, err)
			}
		})
	}
}
