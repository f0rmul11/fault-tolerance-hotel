-- +goose Up
create table public.outbox_messages (
    transaction_id xid8 not null default pg_current_xact_id(),
    id             bigint generated always as identity,
    topic          text not null,
    partition_id   integer not null,
    key            bytea not null,
    body           bytea not null,
    headers        bytea,
    primary key (topic, partition_id, transaction_id, id)
) partition by range (transaction_id);

-- Текущий диапазон XID кластера и три следующих. Границы полуоткрытые.
-- +goose StatementBegin
do $$
    declare
        span constant numeric := 1000000;
        lo numeric := div(pg_current_xact_id()::text::numeric, span) * span;
    begin
        for i in 1..4 loop
                execute format(
                        'create table public.%I partition of public.outbox_messages for values from (%L) to (%L)',
                        'outbox_messages_x' || lo, lo, lo + span
                        );
                lo := lo + span;
            end loop;
    end;
$$;
-- +goose StatementEnd

-- При опоздании ротации запись брони и события всё ещё может завершиться.
create table public.outbox_messages_default partition of public.outbox_messages default;

-- +goose Down
drop table public.outbox_messages;
