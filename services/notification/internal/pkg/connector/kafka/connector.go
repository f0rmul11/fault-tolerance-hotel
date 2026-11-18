package kafka

import (
	"time"

	"github.com/IBM/sarama"
)

func NewConsumerGroup(brokers []string, name string) (sarama.ConsumerGroup, error) {
	cfg := sarama.NewConfig()

	cfg.Consumer.Group.Rebalance.GroupStrategies = []sarama.BalanceStrategy{sarama.NewBalanceStrategyRoundRobin()}

	cfg.Consumer.Group.Rebalance.Retry.Max = 4                          // количество повторов при ребалансе
	cfg.Consumer.Group.Rebalance.Retry.Backoff = 500 * time.Millisecond // интервал между попытками

	cfg.Consumer.Group.Session.Timeout = 30 * time.Second   // таймаут сессии (heartbeat)
	cfg.Consumer.Group.Heartbeat.Interval = 5 * time.Second // heartbeat для координации

	cfg.Consumer.Return.Errors = true
	cfg.Consumer.Offsets.AutoCommit.Enable = false
	cfg.Consumer.Offsets.Initial = sarama.OffsetOldest

	cfg.Consumer.Fetch.Min = 1
	cfg.Consumer.Fetch.Default = 1024 * 1024  // размер батча для чтения
	cfg.Consumer.Fetch.Max = 10 * 1024 * 1024 // максимум за один fetch
	cfg.Consumer.MaxProcessingTime = 500 * time.Millisecond

	return sarama.NewConsumerGroup(brokers, name, cfg)
}

func NewProducer(brokers []string) (sarama.SyncProducer, error) {
	cfg := sarama.NewConfig()
	cfg.Producer.Partitioner = sarama.NewManualPartitioner
	cfg.Producer.RequiredAcks = sarama.WaitForAll
	cfg.Producer.Return.Successes = true
	// В служебную запись к исходному сообщению добавляются заголовки повтора.
	cfg.Producer.MaxMessageBytes = 2 << 20

	return sarama.NewSyncProducer(brokers, cfg)
}
