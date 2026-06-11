-- +goose Up
CREATE TABLE api_keys (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    key_hash TEXT NOT NULL UNIQUE,
    subject TEXT NOT NULL,
    name TEXT NOT NULL,
    roles TEXT[] NOT NULL DEFAULT '{member}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ
);

CREATE INDEX api_keys_subject_idx ON api_keys (subject);

-- +goose Down
DROP TABLE api_keys;
