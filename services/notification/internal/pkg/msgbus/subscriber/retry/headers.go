// Package retry manages the Kafka headers used for delayed processing.
package retry

import (
	"errors"
	"strconv"
	"time"

	"github.com/IBM/sarama"
)

func Read(raw []*sarama.RecordHeader) map[string]string {
	values := make(map[string]string, len(raw))
	for _, h := range raw {
		if h != nil {
			values[string(h.Key)] = string(h.Value)
		}
	}
	return values
}

func Write(values map[string]string) []sarama.RecordHeader {
	raw := make([]sarama.RecordHeader, 0, len(values))
	for key, value := range values {
		raw = append(raw, sarama.RecordHeader{
			Key:   []byte(key),
			Value: []byte(value),
		})
	}
	return raw
}

func setOrigin(h map[string]string, m *sarama.ConsumerMessage, started time.Time) {
	h["original-topic"] = m.Topic
	h["original-partition"] = strconv.FormatInt(int64(m.Partition), 10)
	h["original-offset"] = strconv.FormatInt(m.Offset, 10)
	h["original-timestamp"] = m.Timestamp.UTC().Format(time.RFC3339Nano)
	h["first-attempt"] = started.UTC().Format(time.RFC3339Nano)
	delete(h, "replayed-at")
	delete(h, "replay-id")
	delete(h, "replay-of")
}

// DeliverAt ignores scheduling hints on the main topic. Only a retry subscription waits.
func (r *Router) DeliverAt(h map[string]string, m *sarama.ConsumerMessage, now time.Time) (time.Time, error) {
	if r.attempt == 0 {
		delete(h, "retry-count")
		return time.Time{}, nil
	}
	offset, err := strconv.ParseInt(h["original-offset"], 10, 64)
	if err != nil || offset < 0 || h["original-topic"] != r.originalTopic || h["original-partition"] != strconv.FormatInt(int64(m.Partition), 10) {
		return time.Time{}, errors.New("invalid original record coordinates")
	}
	if h["retry-count"] != strconv.Itoa(r.attempt) {
		return time.Time{}, errors.New("retry count does not match the subscribed stage")
	}
	due, err := time.Parse(time.RFC3339Nano, h["deliver-at"])
	// The largest permitted backoff is 1h + 20% jitter. A damaged timestamp
	// must never tie up the retry partition indefinitely.
	if err != nil || due.After(now.Add(2*time.Hour)) {
		return time.Time{}, errors.New("invalid retry delivery time")
	}
	return due, nil
}
