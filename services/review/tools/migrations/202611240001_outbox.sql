-- +goose Up
create table public.outbox (
    id bigserial primary key,
    entity_id text,
    topic text,
    key bytea,
    body bytea,
    headers jsonb,
    metadata jsonb,
    created_at timestamp default current_timestamp,
    processed_at timestamp,
    errors_count integer default 0,
    error_desc text
);

create index outbox_pending_id_idx on public.outbox (id) where processed_at is null;

-- +goose Down
drop table public.outbox;
