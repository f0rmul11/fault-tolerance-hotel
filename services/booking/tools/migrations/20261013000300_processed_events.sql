-- +goose Up
create table public.processed_events (
    correlation_id uuid primary key
);
-- +goose Down
drop table public.processed_events;
