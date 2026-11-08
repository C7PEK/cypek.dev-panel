-- +goose Up
-- Per-server Discord alert settings; the global ones live in settings
-- under the key "alerts".
CREATE TABLE server_alerts (
    server_id TEXT    PRIMARY KEY REFERENCES servers(id) ON DELETE CASCADE,
    enabled   INTEGER NOT NULL DEFAULT 1,
    webhook   TEXT    NOT NULL DEFAULT '', -- sealed with the encryption key
    mention   TEXT    NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE server_alerts;
