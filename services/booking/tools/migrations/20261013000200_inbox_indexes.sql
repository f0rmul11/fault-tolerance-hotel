-- +goose Up
create unique index idx_inbox_correlation_id_topic on inbox (correlation_id, topic);
create index idx_inbox_id_processed_at_error_counter on inbox (id) include (topic) where processed_at is null;

-- +goose Down
drop index idx_inbox_id_processed_at_error_counter;
drop index idx_inbox_correlation_id_topic;
