-- +goose Up
create table public.checkins (
    id uuid primary key,
    booking_id uuid unique,
    room_id bigint,
    room_number text,
    room_type text,
    guest_id uuid,
    starts_at timestamptz,
    ends_at timestamptz,
    status text,
    previous_status text,
    version bigint,
    actor_id uuid,
    request_id uuid,
    requested_at timestamptz,
    failure text
);
create index checkins_arrivals_idx on public.checkins (starts_at, id) where status in ('EXPECTED','ARRIVING');
-- Технические ключи HTTP-идемпотентности; не бизнес-сущность.
create table public.requests (
    actor_id uuid,
    key uuid,
    booking_id uuid,
    action text,
    primary key (actor_id, key)
);
-- +goose Down
drop table public.requests;
drop table public.checkins;
