-- +goose Up
CREATE TABLE server_databases (
    id         INTEGER PRIMARY KEY,
    server_id  TEXT    NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    name       TEXT    NOT NULL UNIQUE,
    label      TEXT    NOT NULL DEFAULT '',
    password   TEXT    NOT NULL,
    created_at TEXT    NOT NULL
);
CREATE INDEX server_databases_server ON server_databases(server_id);

-- +goose Down
DROP TABLE server_databases;
