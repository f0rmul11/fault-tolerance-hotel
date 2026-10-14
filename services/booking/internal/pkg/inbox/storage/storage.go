package storage

import (
	"context"
	"time"

	"booking/internal/pkg/inbox/message"
	"booking/internal/pkg/transaction/wrapper"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lib/pq"
	"github.com/samber/lo"
)

type Storage struct {
	wrapper wrapper.Database
}

func NewStorage(pool *pgxpool.Pool) *Storage {
	return &Storage{wrapper: wrapper.NewDatabase(pool)}
}

// SaveMessages сохраняет сообщения для дальнейшей отправки в kafka
func (s Storage) SaveMessages(ctx context.Context, messages message.Messages) error {
	sql := `with income_messages as (
                      select unnest($1::text[])      as entity_id,
                             unnest($2::text[])      as correlation_id,
                             unnest($3::text[])      as topic,
							 unnest($4::bytea[])     as key,
                             unnest($5::jsonb[])     as header,
							 unnest($6::jsonb[])     as metadata,
                             unnest($7::bytea[])     as body,
                             unnest($8::timestamp[]) as created_at
					  ),
					  unique_messages as (
								 select distinct on (im.correlation_id,im.topic)
									 im.entity_id,
									 im.correlation_id,
									 im.topic,
									 im.key,
									 im.header,
									 im.metadata,
									 im.body,
									 im.created_at
								 from income_messages as im
								 left outer join inbox as ki
								 on im.correlation_id = ki.correlation_id and im.topic = ki.topic
								 where ki.correlation_id is null
					  )
				insert into inbox (entity_id, correlation_id, topic, key, header, metadata, body, created_at)
				select entity_id, correlation_id, topic, key, header, metadata, body, created_at
				from unique_messages;`

	_, err := s.wrapper.Pool(ctx).Exec(ctx, sql,
		messages.EntitiesIDs(),
		messages.CorrelationIDs(),
		messages.Topics(),
		messages.Keys(),
		messages.Headers(),
		messages.Metadata(),
		messages.Bodies(),
		messages.CreatedAt(),
	)
	return err
}

// GetPendingMessages возвращает пачку сообщений, которые еще не были обработаны до-конца
func (s Storage) GetPendingMessages(ctx context.Context, topic string, messagesLimit int) (message.Messages, error) {
	sql := `with error_messages as(
			select distinct entity_id from inbox
			where topic = $1 and processed_at is null and errors_count > 0
		)
		select * from inbox k
		where topic = $1
		and processed_at is null
		and entity_id not in (select entity_id from error_messages)
		order by id
		limit $2;` // получаем необработанные сообщения по топику

	var messages []*Message

	err := pgxscan.Select(ctx, s.wrapper.Pool(ctx), &messages, sql, topic, messagesLimit)
	if err != nil {
		return nil, err
	}

	return lo.Map(messages, func(msg *Message, _ int) *message.Message {
		return msg.Convert()
	}), nil
}

// GetBrokenMessages получаем сообщения с error_count > 0 and processed_at is null
func (s Storage) GetBrokenMessages(ctx context.Context, topic string, messagesLimit int) (message.Messages, error) {
	sql := `select * from inbox
			 where topic = $1
			 and processed_at is null and errors_count > 0
			 order by id
			 limit $2;`

	var messages []*Message

	err := pgxscan.Select(ctx, s.wrapper.Pool(ctx), &messages, sql, topic, messagesLimit)
	if err != nil {
		return nil, err
	}

	return lo.Map(messages, func(msg *Message, _ int) *message.Message {
		return msg.Convert()
	}), nil
}

// MarkAsProcessed помечает сообщения обработанными
func (s Storage) MarkAsProcessed(ctx context.Context, messages message.Messages) error {
	var (
		messagesIDs = lo.Map(messages, func(m *message.Message, _ int) int64 {
			return m.GetID()
		})
		messagesProcessedAt = lo.Map(messages, func(m *message.Message, _ int) *time.Time {
			return m.GetProcessedAt()
		})
		messagesErrorsCount = lo.Map(messages, func(m *message.Message, _ int) int32 {
			return m.GetErrorsCount()
		})
		messagesErrorsDesc = lo.Map(messages, func(m *message.Message, _ int) *string {
			return m.GetErrorDesc()
		})
	)

	sql := `update inbox
			set
				processed_at = tmp.processed_at,
			    errors_count = tmp.errors_count,
			    error_desc   = tmp.errors_desc
			from (
				select unnest($1::bigint[]) as id,
                       unnest($2::timestamp[]) as processed_at,
                       unnest($3::integer[]) as errors_count,
                       unnest($4::text[]) as errors_desc
			) as tmp
			where inbox.id = tmp.id`

	_, err := s.wrapper.Pool(ctx).Exec(ctx, sql,
		pq.Array(messagesIDs),
		pq.Array(messagesProcessedAt),
		pq.Array(messagesErrorsCount),
		pq.Array(messagesErrorsDesc),
	)
	return err
}
