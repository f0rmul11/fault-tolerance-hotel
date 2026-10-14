-- +goose Up
create extension if not exists btree_gist;

create table public.bookings (
    id uuid primary key,
    guest_id uuid,
    room_id bigint not null,
    room_number text,
    room_type text,
    starts_at timestamptz not null,
    ends_at timestamptz not null,
    request_id uuid,
    status text,
    idempotency_key uuid,
    version bigint,
    created_at timestamptz,
    updated_at timestamptz,

    unique (guest_id, idempotency_key),

    constraint bookings_period_valid check (isfinite(starts_at) and isfinite(ends_at) and starts_at < ends_at),
    constraint bookings_room_period_excl exclude using gist (
        room_id with =,
        tstzrange(starts_at, ends_at, '[)') with &&
    ) where (status is distinct from 'CANCELED')
);
create index bookings_guest_idx on public.bookings (guest_id, created_at);

-- +goose Down
drop table public.bookings;
-- btree_gist остаётся установленным в собственной БД Booking.
