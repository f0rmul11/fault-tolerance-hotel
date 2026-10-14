package kafka

import (
	"log"
	"time"

	"check-in/config"

	"github.com/IBM/sarama"
)

func MustSyncProducer() sarama.SyncProducer {
	saramaConfig := sarama.NewConfig()

	saramaConfig.Producer.RequiredAcks = sarama.WaitForAll
	saramaConfig.Producer.Return.Successes = true // для синхронного producer'а

	saramaConfig.Net.DialTimeout = 5 * time.Second
	saramaConfig.Net.WriteTimeout = 5 * time.Second

	saramaConfig.Producer.Retry.Max = 10 // максимальное количество повторов при ошибках
	saramaConfig.Producer.Retry.Backoff = 100 * time.Millisecond
	saramaConfig.Producer.Idempotent = true
	saramaConfig.Producer.Partitioner = sarama.NewHashPartitioner

	saramaConfig.Producer.Timeout = 10 * time.Second // таймаут на запись
	saramaConfig.Net.MaxOpenRequests = 1             // для идемпотентного продьюсера MaxOpenRequests должен быть 1

	saramaConfig.Metadata.Retry.Max = 5
	saramaConfig.Metadata.Retry.Backoff = 500 * time.Millisecond

	producer, err := sarama.NewSyncProducer(config.Instance().KafkaBrokers, saramaConfig)
	if err != nil {
		log.Fatalf("[KAFKA] Не удалось создать producer: %v", err)
		return nil
	}

	return producer
}

func MustConsumerGroup() sarama.ConsumerGroup {
	cfg := sarama.NewConfig()

	cfg.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{sarama.NewBalanceStrategyRoundRobin()}

	cfg.Consumer.Group.Rebalance.Retry.Max = 4                          // количество повторов при ребалансе
	cfg.Consumer.Group.Rebalance.Retry.Backoff = 500 * time.Millisecond // интервал между попытками

	cfg.Consumer.Group.Session.Timeout = 30 * time.Second   // таймаут сессии (heartbeat)
	cfg.Consumer.Group.Heartbeat.Interval = 5 * time.Second // heartbeat для координации

	cfg.Consumer.Offsets.AutoCommit.Enable = false
	cfg.Consumer.Offsets.Initial = sarama.OffsetOldest

	cfg.Consumer.Fetch.Min = 1
	cfg.Consumer.Fetch.Default = 1024 * 1024  // размер батча для чтения
	cfg.Consumer.Fetch.Max = 10 * 1024 * 1024 // максимум за один fetch
	cfg.Consumer.MaxProcessingTime = 500 * time.Millisecond

	group, err := sarama.NewConsumerGroup(config.Instance().KafkaBrokers, config.Instance().ConsumerGroup, cfg)
	if err != nil {
		log.Fatalf("Не удалось создать Kafka consumer: %v", err)
		return nil
	}

	return group
}
