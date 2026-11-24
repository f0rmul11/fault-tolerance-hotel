-- +goose Up
create table public.ratings (
 hotel_id uuid primary key,
 total_stars bigint,
 review_count bigint,
 version bigint default 1,
 updated_at timestamptz default current_timestamp
);
-- +goose Down
drop table public.ratings;
