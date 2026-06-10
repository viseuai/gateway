-- +goose Up
CREATE TABLE usage_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    occurred_at TIMESTAMPTZ NOT NULL,
    subject TEXT NOT NULL,
    model TEXT NOT NULL DEFAULT '',
    route TEXT NOT NULL,
    status INT NOT NULL,
    duration_ms BIGINT NOT NULL,
    prompt_tokens INT,
    completion_tokens INT,
    streamed BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX usage_events_subject_time_idx
    ON usage_events (subject, occurred_at);

-- +goose Down
DROP TABLE usage_events;
