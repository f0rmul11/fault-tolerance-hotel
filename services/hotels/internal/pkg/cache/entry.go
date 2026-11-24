package cache

import (
	"encoding/binary"
	"time"

	"github.com/samber/lo"
)

// Entries ...
type Entries[TValue Marshaller, TValuePtr UnMarshaller] []Entry[TValue, TValuePtr]

// Entry запись в кеше
type Entry[TValue Marshaller, TValuePtr UnMarshaller] struct {
	Key        string
	Value      TValue
	Expiration time.Duration
	Version    uint32

	isTombstone bool
}

// From создает новый объект
func From[TValue Marshaller, TValuePtr UnMarshaller](key string, rawValue []byte) (Entry[TValue, TValuePtr], error) {
	// распаковываем версию и значение
	version, value := versionUnpack(rawValue)
	if len(value) == 0 {
		return Entry[TValue, TValuePtr]{
			Key:         key,
			Version:     version,
			isTombstone: true, // key is tombstone
		}, nil
	}

	var result TValue
	if err := unmarshal[TValuePtr](value, &result); err != nil {
		return Entry[TValue, TValuePtr]{}, err
	}

	return Entry[TValue, TValuePtr]{
		Key:     key,
		Value:   result,
		Version: version,
	}, nil
}

// NewTombstone создает tombstone entry
func NewTombstone[TValue Marshaller, TValuePtr UnMarshaller](key string, exp time.Duration) Entry[TValue, TValuePtr] {
	return Entry[TValue, TValuePtr]{
		Key:         key,
		Expiration:  exp,
		isTombstone: true,
	}
}

func (e *Entry[TValue, TValuePtr]) TTL() time.Duration {
	return e.Expiration
}

// IsTombstone проверяет, является ли запись мертвой
func (e *Entry[TValue, TValuePtr]) IsTombstone() bool {
	return e.isTombstone
}

// Marshall конвертирует значение в байты
func (e *Entry[TValue, TValuePtr]) marshall() []byte {
	bytes, _ := marshall[TValue](e.Value)
	return bytes
}

// Map возвращает ключи в виде map
func (e Entries[TValue, TValuePtr]) Map() map[string]Entry[TValue, TValuePtr] {
	return lo.SliceToMap(e, func(entry Entry[TValue, TValuePtr]) (string, Entry[TValue, TValuePtr]) {
		return entry.Key, entry
	})
}

// versionUnpack распаковывает из значения версию ключа
var versionUnpack = func(value []byte) (uint32, []byte) {
	if len(value) < 4 {
		return 0, nil
	}

	version := binary.LittleEndian.Uint32(value[:4])
	value = value[4:]

	return version, value
}

// versionPack упаковывает версию и данные
var versionPack = func(version uint32, data []byte) []byte {
	buf := make([]byte, 4+len(data))
	binary.LittleEndian.PutUint32(buf[0:4], version)
	copy(buf[4:], data)
	return buf
}
