-- +goose Up
CREATE TABLE node_models (
    node TEXT NOT NULL,
    subject TEXT NOT NULL,
    model TEXT NOT NULL,
    url TEXT NOT NULL,
    last_seen TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (node, model)
);

CREATE INDEX node_models_model_seen_idx ON node_models (model, last_seen DESC);

-- +goose Down
DROP TABLE node_models;
