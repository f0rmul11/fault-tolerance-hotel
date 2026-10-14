insert into public.ratings (hotel_id, total_stars, review_count) values
 ('cdd4685e-c610-4e7e-95ee-c51fb6807400', 45, 10),
 ('abca2095-b122-4323-bb20-459d2cc86818', 16, 4)
on conflict (hotel_id) do nothing;
