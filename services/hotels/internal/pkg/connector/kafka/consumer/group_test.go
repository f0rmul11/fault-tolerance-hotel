package consumer

import (
	"context"
	"errors"
	"testing"

	"github.com/IBM/sarama"
)

type sessionFake struct {
	sarama.ConsumerGroupSession
	ctx     context.Context
	marked  int
	commits int
}

func (s *sessionFake) Context() context.Context {
	return s.ctx
}

func (s *sessionFake) MarkMessage(*sarama.ConsumerMessage, string) {
	s.marked++
}

func (s *sessionFake) Commit() {
	s.commits++
}

type claimFake struct {
	sarama.ConsumerGroupClaim
	messages chan *sarama.ConsumerMessage
}

func (c claimFake) Messages() <-chan *sarama.ConsumerMessage {
	return c.messages
}

func TestCommitOnlyAfterSuccessfulInvalidation(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[fail], func(t *testing.T) {
			session := &sessionFake{ctx: t.Context()}
			messages := make(chan *sarama.ConsumerMessage, 1)
			messages <- &sarama.ConsumerMessage{Topic: "review.rating.events.v1"}
			close(messages)

			handlerErr := errors.New("invalidation failed")
			subscriber := groupSubscriber{
				messageHandler: func(context.Context, sarama.ConsumerGroupSession, *sarama.ConsumerMessage) error {
					if session.marked != 0 || session.commits != 0 {
						t.Fatal("offset committed before handler")
					}
					if fail {
						return handlerErr
					}
					return nil
				},
			}

			err := subscriber.ConsumeClaim(session, claimFake{messages: messages})
			if fail {
				if !errors.Is(err, handlerErr) || session.marked != 0 || session.commits != 0 {
					t.Fatalf("failed handler advanced offset: %v", err)
				}
			} else if err != nil || session.marked != 1 || session.commits != 1 {
				t.Fatalf("successful handler did not commit: %v", err)
			}
		})
	}
}
