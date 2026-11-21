-- +goose Up
-- Миграция сервиса добавляет по одному курсору на каждую партицию топика.
create table public.outbox_offsets (
    topic        text not null,
    partition_id integer not null,
    last_xid     xid8 not null default '0',
    last_id      bigint not null default 0,
    primary key (topic, partition_id)
) with (fillfactor = 70);

-- +goose Down
drop table public.outbox_offsets;
