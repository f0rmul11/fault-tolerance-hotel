package cache

import (
	"context"
	"fmt"
	"time"

	"hotels/internal/pkg/hotelserror"

	"github.com/pkg/errors"
	"github.com/samber/lo"
	"github.com/valkey-io/valkey-go"
)

const startFrom = 1

type Client[TValue Marshaller, TValuePtr UnMarshaller] struct {
	client valkey.Client
}

func NewClient[TValue Marshaller, TValuePtr UnMarshaller](client valkey.Client) *Client[TValue, TValuePtr] {
	return &Client[TValue, TValuePtr]{
		client: client,
	}
}

func (c *Client[TValue, TValuePtr]) Get(ctx context.Context, key string) (Entry[TValue, TValuePtr], error) {
	value, err := c.client.Do(ctx, c.client.B().Get().Key(key).Build()).AsBytes()
	if err != nil {
		return Entry[TValue, TValuePtr]{}, fmt.Errorf("get entry for key %s: %w", key, err)
	}
	return From[TValue, TValuePtr](key, value)
}

func (c *Client[TValue, TValuePtr]) Set(ctx context.Context, entry Entry[TValue, TValuePtr]) error {
	packedValue := versionPack(entry.Version, entry.marshall())
	return c.client.Do(ctx, setCommand(c.client.B(), entry.Key, packedValue, entry.TTL())).Error()
}

func (c *Client[TValue, TValuePtr]) TryLock(ctx context.Context, entry Entry[TValue, TValuePtr]) (bool, error) {
	builder := c.client.B().Set().Key(entry.Key).Value(string(entry.marshall())).Nx()
	var command valkey.Completed
	if entry.TTL() > 0 {
		command = builder.PxMilliseconds(max(1, entry.TTL().Milliseconds())).Build()
	} else {
		command = builder.Build()
	}
	err := c.client.Do(ctx, command).Error()
	if valkey.IsValkeyNil(err) {
		return false, nil
	}
	return err == nil, err
}

func (c *Client[TValue, TValuePtr]) Unlock(ctx context.Context, entry Entry[TValue, TValuePtr]) error {
	script := valkey.NewLuaScript(`
		if redis.call("get", KEYS[1]) == ARGV[1] then
			return redis.call("del", KEYS[1])
		else
			return 0
		end
	`)
	return script.Exec(ctx, c.client, []string{entry.Key}, []string{string(entry.marshall())}).Error()
}

// Cas модифицирует значение по ключу, увеличивая его версию
// При конкурентной ситуации выдаст ошибку
func (c *Client[TValue, TValuePtr]) Cas(ctx context.Context, entry Entry[TValue, TValuePtr]) (uint32, error) {
	var version uint32
	tx, release := c.client.Dedicate()
	err := func() error {
		if err := tx.Do(ctx, tx.B().Watch().Key(entry.Key).Build()).Error(); err != nil {
			return err
		}
		// получаем текущее значение
		packed, err := tx.Do(ctx, tx.B().Get().Key(entry.Key).Build()).AsBytes()
		// если ключа нет - мы его создадим
		if err != nil && !errors.Is(err, valkey.Nil) {
			return errors.WithMessage(err, "cas: get key")
		}

		exists := !errors.Is(err, valkey.Nil)
		// распаковываем версию и само значение (которое не особо то и интересно)
		version, _ = versionUnpack(packed)

		// если ключик существует и нас попросили сделать обновление (передали какую-то версию),
		// то надо понять, а можно ли это делать?
		if entry.Version != 0 {
			// Исчезновение ключа тоже аннулирует ранее полученный токен.
			if !exists || version != entry.Version {
				return hotelserror.NewConflictErr("modification conflict") // Конкурентная модификация
			}
		}

		if entry.Version == 0 && !entry.IsTombstone() && exists {
			return hotelserror.NewConflictErr("key unexpected exists")
		}

		// если ключа нет - его версия будет `startFrom`, в ином случае increment
		version = lo.Ternary(!exists, startFrom, version+1)

		var value []byte
		if !entry.IsTombstone() {
			value = entry.marshall()
		}

		packedValue := versionPack(version, value)

		results := tx.DoMulti(ctx,
			tx.B().Multi().Build(),
			setCommand(tx.B(), entry.Key, packedValue, entry.TTL()),
			tx.B().Exec().Build(),
		)
		commands, err := results[2].ToArray()
		if err != nil {
			return err
		}
		return commands[0].Error()
	}()
	tx.Close()
	release()
	if err != nil {
		if errors.Is(err, valkey.Nil) {
			return 0, hotelserror.NewConflictErr("modification conflict") // Конкурентная модификация
		}
		return 0, err
	}
	return version, nil
}

func setCommand(builder valkey.Builder, key string, value []byte, ttl time.Duration) valkey.Completed {
	command := builder.Set().Key(key).Value(string(value))
	if ttl > 0 {
		return command.PxMilliseconds(max(1, ttl.Milliseconds())).Build()
	}
	if ttl == -1 {
		return command.Keepttl().Build()
	}
	return command.Build()
}

// CasNative заменяет ожидаемое значение через SET IFEQ без упаковки версии.
// Оба Entry относятся к одному ключу; ключ и положительный TTL берутся из updated.
// Значение в Valkey должно быть обычным JSON, а не форматом versionPack ручного Cas.
func (c *Client[TValue, TValuePtr]) CasNative(ctx context.Context, expected, updated Entry[TValue, TValuePtr]) error {
	err := c.client.Do(ctx,
		c.client.B().Set().
			Key(updated.Key).
			Value(string(updated.marshall())).
			Ifeq(string(expected.marshall())).
			Px(updated.TTL()).
			Build(),
	).Error()

	if valkey.IsValkeyNil(err) {
		return hotelserror.NewConflictErr("modification conflict")
	}
	return err
}
