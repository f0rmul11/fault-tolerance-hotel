-- +goose Up
create table public.bookings_state (
    guest_id uuid primary key,
    version bigint
);

-- +goose Down
drop table public.bookings_state;
