package storage

import (
	"context"

	"check-in/internal/pkg/outbox/message"
	"check-in/internal/pkg/transaction/wrapper"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lib/pq"
	"github.com/samber/lo"
)

type Storage struct {
	wrapper wrapper.Database
}

func NewStorage(pool *pgxpool.Pool) *Storage {
	return &Storage{
		wrapper: wrapper.NewDatabase(pool),
	}
}

// SaveMessages сохраняет сообщения для дальнейшей отправки в kafka
func (s Storage) SaveMessages(ctx context.Context, messages message.Messages) error {
	sql := `insert into outbox (entity_id, topic,key,body,headers,metadata,created_at)
				select unnest($1::text[]) 	as entity_id,
					   unnest($2::text[])  		as topic,
					   unnest($3::bytea[])  	as key,
					   unnest($4::bytea[])  	as body,
					   unnest($5::jsonb[])  	as headers,
					   unnest($6::jsonb[]) 		as metadata,
					   unnest($7::timestamp[]) as created_at;`

	_, err := s.wrapper.Pool(ctx).Exec(ctx, sql,
		messages.EntitiesIDs(),
		messages.Topics(),
		messages.Keys(),
		messages.Bodies(),
		messages.Headers(),
		messages.Metadata(),
		messages.CreatedAt(),
	)
	return err
}

// MarkAsProcessed помечает батч сообщений как обработанный
func (s Storage) MarkAsProcessed(ctx context.Context, messages message.Messages) error {
	sql := `update outbox
			set processed_at = tmp.processed_at,
				errors_count = tmp.errors_count,
				error_desc   = tmp.error_desc
			from (select unnest($1::bigint[])    as id,
						 unnest($2::timestamp[]) as processed_at,
						 unnest($3::integer[])   as errors_count,
						 unnest($4::text[])      as error_desc) as tmp
			where outbox.id = tmp.id;`

	_, err := s.wrapper.Pool(ctx).Exec(ctx, sql,
		pq.Array(messages.IDs()),
		pq.Array(messages.ProcessedAt()),
		pq.Array(messages.ErrorsCounts()),
		pq.Array(messages.ErrorsDesc()),
	)
	return err
}

// GetPendingMessages возвращает батч необработанных сообщений в разрезе топика
func (s Storage) GetPendingMessages(ctx context.Context, keysLimit, messagesLimit int) (batch message.Messages, _ error) {
	lockSql := `with target_rows(entity_id) as (
                select entity_id from outbox where processed_at is null
                order by id + 0
                limit $1
            )
			select entity_id from target_rows
			where pg_try_advisory_xact_lock(hashtext(entity_id));`

	var lockedKeys []string
	// блокируем ключи
	err := pgxscan.Select(ctx, s.wrapper.Pool(ctx), &lockedKeys, lockSql, keysLimit)
	if err != nil {
		return nil, err
	}

	sql := `select * from outbox where
            outbox.entity_id = any ($1)
            and outbox.processed_at is null
			order by id
			limit $2;`

	var messages []*Message

	err = pgxscan.Select(ctx, s.wrapper.Pool(ctx), &messages, sql,
		pq.Array(lockedKeys),
		messagesLimit,
	)
	if err != nil {
		return nil, err
	}

	return lo.Map(messages, func(msg *Message, _ int) *message.Message {
		return msg.Convert()
	}), nil
}
