-- +goose Up
CREATE TABLE server_expiry (
    server_id  TEXT PRIMARY KEY REFERENCES servers(id) ON DELETE CASCADE,
    expires_at TEXT NOT NULL,
    expired_at TEXT NOT NULL DEFAULT '',
    warned_at  TEXT NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE server_expiry;
