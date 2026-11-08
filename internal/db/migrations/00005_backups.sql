-- +goose Up
CREATE TABLE backups (
    id          INTEGER PRIMARY KEY,
    server_id   TEXT    NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    file_name   TEXT    NOT NULL,
    size        INTEGER NOT NULL DEFAULT 0,
    note        TEXT    NOT NULL DEFAULT '',
    trigger     TEXT    NOT NULL DEFAULT 'manual',   -- manual | schedule
    created_by  INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at  TEXT    NOT NULL
);
CREATE INDEX backups_server ON backups(server_id, id);

-- Per-server backup settings: {"excludes":["cache/","logs/"],"keepLast":7,"keepDays":30,"flushMinecraft":true}
ALTER TABLE servers ADD COLUMN backup_settings TEXT NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE servers DROP COLUMN backup_settings;
DROP TABLE backups;
