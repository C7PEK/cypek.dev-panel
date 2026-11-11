-- +goose Up
CREATE TABLE server_alerts (
    server_id TEXT    PRIMARY KEY REFERENCES servers(id) ON DELETE CASCADE,
    enabled   INTEGER NOT NULL DEFAULT 1,
    webhook   TEXT    NOT NULL DEFAULT '',
    mention   TEXT    NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE server_alerts;
