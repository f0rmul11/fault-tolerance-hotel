-- +goose Up
-- +goose StatementBegin
create table if not exists inbox
(
    id                        bigserial not null primary key,
    entity_id                 text      not null,
    correlation_id            text      not null,
    topic                     text      not null,
    key                       bytea     not null,
    body                      bytea     not null,
    header                    jsonb, --nullable
    metadata                  jsonb, --nullable
    created_at                timestamp default current_timestamp not null,
    processed_at              timestamp, -- nullable
    errors_count              integer not null default 0,
    error_desc                text -- nullable
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop table inbox;
-- +goose StatementEnd
