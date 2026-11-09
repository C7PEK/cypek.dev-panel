-- +goose Up
-- Optional end date of a server (lent to a friend for 30 days...). When it
-- passes the panel stops and locks the server; it is never deleted
-- automatically, an admin extends or deletes it.
CREATE TABLE server_expiry (
    server_id  TEXT PRIMARY KEY REFERENCES servers(id) ON DELETE CASCADE,
    expires_at TEXT NOT NULL,
    expired_at TEXT NOT NULL DEFAULT '',
    warned_at  TEXT NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE server_expiry;
