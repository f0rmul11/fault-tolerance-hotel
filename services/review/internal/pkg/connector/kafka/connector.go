package kafka

import (
	"fmt"
	"time"

	"github.com/IBM/sarama"

	"review/config"
)

func NewSyncProducer() (sarama.SyncProducer, error) {
	saramaConfig := sarama.NewConfig()

	saramaConfig.Producer.RequiredAcks = sarama.WaitForAll
	saramaConfig.Producer.Return.Successes = true // для синхронного producer'а

	saramaConfig.Net.DialTimeout = 5 * time.Second
	saramaConfig.Net.WriteTimeout = 5 * time.Second
	saramaConfig.Net.ReadTimeout = 5 * time.Second
	saramaConfig.Metadata.AllowAutoTopicCreation = false

	saramaConfig.Producer.Retry.Max = 3 // максимальное количество повторов при ошибках
	saramaConfig.Producer.Retry.Backoff = 100 * time.Millisecond
	saramaConfig.Producer.Idempotent = true
	saramaConfig.Producer.Partitioner = sarama.NewHashPartitioner

	saramaConfig.Producer.Timeout = 10 * time.Second // таймаут на запись
	saramaConfig.Net.MaxOpenRequests = 1             // Sarama требует 1 при включённой идемпотентности

	saramaConfig.Metadata.Retry.Max = 5
	saramaConfig.Metadata.Retry.Backoff = 500 * time.Millisecond

	producer, err := sarama.NewSyncProducer(config.Instance().KafkaBrokers, saramaConfig)
	if err != nil {
		return nil, fmt.Errorf("[KAFKA] Не удалось создать producer: %w", err)
	}

	return producer, nil
}
