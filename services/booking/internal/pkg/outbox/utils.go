package outbox

import "hash/fnv"

// partitionByKey повторяет алгоритм sarama.NewHashPartitioner для ключа сообщения.
func partitionByKey(key []byte, count int32) int32 {
	h := fnv.New32a()
	_, _ = h.Write(key)

	// Sarama сначала приводит хеш к знаковому int32, затем берёт остаток.
	partitionID := int32(h.Sum32()) % count
	if partitionID < 0 {
		return -partitionID
	}

	return partitionID
}
