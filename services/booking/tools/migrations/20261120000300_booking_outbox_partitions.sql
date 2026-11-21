-- +goose Up
-- У booking.events.v1 три Kafka-партиции с номерами от 0 до 2.
insert into public.outbox_offsets (topic, partition_id)
select 'booking.events.v1', partition_id
from generate_series(0, 2) as p(partition_id)
on conflict (topic, partition_id) do nothing;

-- +goose Down
-- Сохраняем курсоры при откате, включая позиции, существовавшие до миграции.
select 1;
