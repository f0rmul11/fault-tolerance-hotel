insert into public.hotels (id, name, description, stars, country, city, address, latitude, longitude) values
 ('cdd4685e-c610-4e7e-95ee-c51fb6807400', 'Учебный отель', 'Отель для сценариев HotelFlow', 4, 'Россия', 'Москва', 'Учебная улица, 1', 55.75, 37.62),
 ('abca2095-b122-4323-bb20-459d2cc86818', 'Отель у парка', 'Тихий отель рядом с парком', 3, 'Россия', 'Москва', 'Парковая улица, 2', 55.77, 37.59),
 ('215227d1-7d60-4845-b751-f2e6040a2129', 'Новый отель', 'Ещё нет пользовательских оценок', 0, 'Россия', 'Москва', 'Новая улица, 3', 55.73, 37.61)
on conflict (id) do nothing;

insert into public.rooms (id, hotel_id, number, name, room_type, description, capacity, room_count, bed_count, area_square_meters, amenities, min_duration_seconds, max_duration_seconds)
select n,
 'cdd4685e-c610-4e7e-95ee-c51fb6807400'::uuid, (100 + n)::text,
 'Номер ' || (100 + n)::text, case when n <= 5 then 'Standard' else 'Comfort' end,
 'Номер для комфортного проживания', 2, 1, 2, 24,
 array['air_conditioning', 'tv', 'wifi'], 43200, null
from generate_series(1, 10) n
on conflict (id) do nothing;

insert into public.rooms (id, hotel_id, number, name, room_type, description, capacity, room_count, bed_count, area_square_meters, amenities, min_duration_seconds, max_duration_seconds) values
 (11, 'abca2095-b122-4323-bb20-459d2cc86818', '101', 'Семейный номер', 'Family', 'Две комнаты для семьи', 4, 2, 3, 48, array['tv', 'wifi'], 43200, 604800)
on conflict (id) do nothing;

select setval('public.rooms_id_seq', (select max(id) from public.rooms));
