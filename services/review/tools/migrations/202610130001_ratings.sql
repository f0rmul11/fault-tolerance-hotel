-- +goose Up
create table public.ratings (
 hotel_id uuid primary key,
 total_stars bigint,
 review_count bigint
);
-- +goose Down
drop table public.ratings;
