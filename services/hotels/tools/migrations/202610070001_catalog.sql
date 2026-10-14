-- +goose Up
create table public.hotels (
 id uuid primary key,
 name text,
 description text,
 stars integer,
 country text,
 city text,
 address text,
 latitude double precision,
 longitude double precision
);
create table public.rooms (
 id bigserial primary key,
 hotel_id uuid,
 number text,
 name text,
 room_type text,
 description text,
 capacity integer,
 room_count integer,
 bed_count integer,
 area_square_meters integer,
 amenities text[],
 min_duration_seconds bigint,
 max_duration_seconds bigint,
 unique (hotel_id, number)
);
-- +goose Down
drop table public.rooms;
drop table public.hotels;
