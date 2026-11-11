-- +goose Up
CREATE TABLE servers (
    id          TEXT    PRIMARY KEY,
    name        TEXT    NOT NULL,
    template_id TEXT    NOT NULL DEFAULT 'custom',
    category    TEXT    NOT NULL DEFAULT 'other' CHECK (category IN ('fivem', 'minecraft', 'discord-bot', 'other')),
    autostart   INTEGER NOT NULL DEFAULT 0,
    memory_mb   INTEGER NOT NULL DEFAULT 0,
    cpu_percent INTEGER NOT NULL DEFAULT 0,
    run_config  TEXT    NOT NULL,
    variables   TEXT    NOT NULL DEFAULT '{}',
    ports       TEXT    NOT NULL DEFAULT '[]',
    created_by  INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at  TEXT    NOT NULL,
    updated_at  TEXT    NOT NULL
);

CREATE TABLE server_permissions (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    server_id  TEXT    NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    permission TEXT    NOT NULL,
    PRIMARY KEY (user_id, server_id, permission)
);
CREATE INDEX server_permissions_server ON server_permissions(server_id);

-- +goose Down
DROP TABLE server_permissions;
DROP TABLE servers;
