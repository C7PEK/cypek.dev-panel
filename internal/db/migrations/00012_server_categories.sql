-- +goose NO TRANSACTION
-- +goose Up
PRAGMA foreign_keys = OFF;

BEGIN;

CREATE TABLE servers_new (
    id              TEXT    PRIMARY KEY,
    name            TEXT    NOT NULL,
    template_id     TEXT    NOT NULL DEFAULT 'custom',
    category        TEXT    NOT NULL DEFAULT 'other',
    autostart       INTEGER NOT NULL DEFAULT 0,
    memory_mb       INTEGER NOT NULL DEFAULT 0,
    cpu_percent     INTEGER NOT NULL DEFAULT 0,
    run_config      TEXT    NOT NULL,
    variables       TEXT    NOT NULL DEFAULT '{}',
    ports           TEXT    NOT NULL DEFAULT '[]',
    created_by      INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at      TEXT    NOT NULL,
    updated_at      TEXT    NOT NULL,
    install_state   TEXT    NOT NULL DEFAULT 'ready' CHECK (install_state IN ('pending', 'installing', 'ready', 'failed')),
    install_error   TEXT    NOT NULL DEFAULT '',
    backup_settings TEXT    NOT NULL DEFAULT '{}'
);

INSERT INTO servers_new (id, name, template_id, category, autostart, memory_mb, cpu_percent, run_config, variables, ports,
                         created_by, created_at, updated_at, install_state, install_error, backup_settings)
SELECT id, name, template_id, category, autostart, memory_mb, cpu_percent, run_config, variables, ports,
       created_by, created_at, updated_at, install_state, install_error, backup_settings
FROM servers;

DROP TABLE servers;

ALTER TABLE servers_new RENAME TO servers;

COMMIT;

PRAGMA foreign_keys = ON;

-- +goose Down
SELECT 1;
