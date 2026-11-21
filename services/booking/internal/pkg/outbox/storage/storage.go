package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/jackc/pgx/v5/pgxpool"

	"booking/internal/pkg/outbox/message"
	"booking/internal/pkg/transaction/abstract"
	"booking/internal/pkg/transaction/wrapper"
)

type Storage struct {
	db wrapper.Database
}

func NewStorage(pool *pgxpool.Pool) *Storage {
	return &Storage{
		db: wrapper.NewDatabase(pool),
	}
}

// SaveMessages добавляет пачку в порядке событий в транзакции вызывающей стороны.
func (s *Storage) SaveMessages(ctx context.Context, messages []message.Message) error {
	if len(messages) == 0 {
		return nil
	}
	if abstract.TxFromContext(ctx) == nil {
		return errors.New("запись в outbox требует транзакции")
	}

	const query = `
		insert into public.outbox_messages (topic, partition_id, key, body, headers)
		select topic, partition_id, key, body, headers
		from unnest(
			$1::text[],
			$2::integer[],
			$3::bytea[],
			$4::bytea[],
			$5::bytea[]
		) with ordinality as m(topic, partition_id, key, body, headers, position)
		order by position
	`

	topics := make([]string, len(messages))
	partitions := make([]int32, len(messages))
	keys := make([][]byte, len(messages))
	bodies := make([][]byte, len(messages))
	headers := make([][]byte, len(messages))
	for i, msg := range messages {
		topics[i] = msg.Topic
		partitions[i] = msg.PartitionID
		keys[i] = msg.Key
		bodies[i] = msg.Body
		headers[i] = msg.Headers
	}

	// PostgreSQL назначает id в порядке событий, transaction_id общий для всей транзакции.
	tag, err := s.db.Pool(ctx).Exec(ctx, query, topics, partitions, keys, bodies, headers)
	if err != nil {
		return err
	}
	if inserted := tag.RowsAffected(); inserted != int64(len(messages)) {
		return fmt.Errorf("в outbox записано %d сообщений из %d", inserted, len(messages))
	}

	return nil
}

// GetPendingMessages блокирует курсор партиции до конца транзакции вызывающей стороны
// и читает до limit следующих сообщений. Позиция в БД при чтении сохраняется.
// Если курсор занят или отсутствует, возвращает пустую страницу.
func (s *Storage) GetPendingMessages(ctx context.Context, topic string, partitionID int32, limit int) (Page, error) {
	if abstract.TxFromContext(ctx) == nil {
		return Page{}, errors.New("выборка из outbox требует транзакции")
	}
	if limit <= 0 {
		return Page{}, errors.New("лимит сообщений outbox должен быть положительным")
	}

	const lockOffset = `
		select
			last_xid as transaction_id,
			last_id as id
		from public.outbox_offsets
		where topic = $1
			and partition_id = $2
		for no key update skip locked
	`

	db := s.db.Pool(ctx)
	var page Page

	err := pgxscan.Get(ctx, db, &page.Start, lockOffset, topic, partitionID)
	if pgxscan.NotFound(err) {
		return Page{}, nil
	}
	if err != nil {
		return Page{}, err
	}

	// Нижняя граница по transaction_id помогает PostgreSQL отсечь старые секции.
	// Читаем строго ниже xmin снимка, чтобы курсор не обогнал незавершённую запись.
	const readPage = `
		select
			transaction_id,
			id,
			topic,
			partition_id,
			key,
			body,
			headers
		from public.outbox_messages
		where topic = $1
			and partition_id = $2
			and transaction_id >= $3
			and (transaction_id, id) > ($3, $4)
			and transaction_id < pg_snapshot_xmin(pg_current_snapshot())
		order by transaction_id, id
		limit $5
	`

	err = pgxscan.Select(ctx, db, &page.Messages, readPage,
		topic, partitionID,
		page.Start.TransactionID, page.Start.ID,
		limit,
	)
	if err != nil {
		return Page{}, err
	}

	return page, nil
}

// SamplePending считает до limit видимых сообщений за подтверждённым курсором.
// Опрос метрики читает курсор без блокировки и включает сообщения, которые relay
// ещё ждёт из-за xmin.
func (s *Storage) SamplePending(ctx context.Context, topic string, partitionID int32, limit int64) (int64, error) {
	const query = `
		select (
			select count(*)
			from (
				select 1
				from public.outbox_messages m
				where m.topic = $1
					and m.partition_id = $2
					and m.transaction_id >= o.last_xid
					and (m.transaction_id, m.id) > (o.last_xid, o.last_id)
				order by m.transaction_id, m.id
				limit $3
			) pending
		)
		from public.outbox_offsets o
		where o.topic = $1
			and o.partition_id = $2
	`

	var pending int64
	err := pgxscan.Get(ctx, s.db.Pool(ctx), &pending, query, topic, partitionID, limit)
	return pending, err
}

// AdvanceOffset подтверждает непрерывный префикс страницы в транзакции GetPendingMessages.
// from — Page.Start, to — позиция последнего сообщения префикса, подтверждённого брокером.
func (s *Storage) AdvanceOffset(ctx context.Context, topic string, partitionID int32, from, to Offset) error {
	if abstract.TxFromContext(ctx) == nil {
		return errors.New("сдвиг оффсета outbox требует транзакции")
	}

	const query = `
		update public.outbox_offsets
		set
			last_xid = $5,
			last_id = $6
		where topic = $1
			and partition_id = $2
			and (last_xid, last_id) = ($3, $4)
			and (last_xid, last_id) < ($5, $6)
	`

	tag, err := s.db.Pool(ctx).Exec(ctx, query,
		topic, partitionID,
		from.TransactionID, from.ID,
		to.TransactionID, to.ID,
	)
	if err != nil {
		return err
	}

	// Обновиться должен ровно один курсор в ожидаемой начальной позиции.
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("не удалось сдвинуть оффсет топика %q партиции %d с (%d, %d) на (%d, %d)",
			topic, partitionID, from.TransactionID, from.ID, to.TransactionID, to.ID,
		)
	}

	return nil
}
