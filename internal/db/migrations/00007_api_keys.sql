-- +goose Up
CREATE TABLE api_keys (
    id           INTEGER PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT    NOT NULL,
    token_hash   TEXT    NOT NULL UNIQUE,
    prefix       TEXT    NOT NULL,                -- first chars, shown in the UI
    scope        TEXT    NOT NULL DEFAULT '["*"]', -- JSON list of permissions or ["*"]
    created_at   TEXT    NOT NULL,
    expires_at   TEXT,
    last_used_at TEXT
);
CREATE INDEX api_keys_user ON api_keys(user_id);

-- +goose Down
DROP TABLE api_keys;
