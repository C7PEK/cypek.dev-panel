-- +goose Up
CREATE TABLE schedules (
    id          INTEGER PRIMARY KEY,
    server_id   TEXT    NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    name        TEXT    NOT NULL,
    cron_expr   TEXT    NOT NULL,
    enabled     INTEGER NOT NULL DEFAULT 1,
    actions     TEXT    NOT NULL,                 -- JSON [{type, ...}]
    only_if_running  INTEGER NOT NULL DEFAULT 1,
    skip_if_players  INTEGER NOT NULL DEFAULT 0,
    created_by  INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at  TEXT    NOT NULL,
    updated_at  TEXT    NOT NULL,
    last_run_at TEXT,
    next_run_at TEXT
);
CREATE INDEX schedules_server ON schedules(server_id);

CREATE TABLE schedule_runs (
    id          INTEGER PRIMARY KEY,
    schedule_id INTEGER NOT NULL REFERENCES schedules(id) ON DELETE CASCADE,
    started_at  TEXT    NOT NULL,
    finished_at TEXT,
    result      TEXT    NOT NULL DEFAULT 'running' CHECK (result IN ('running','ok','skipped','failed')),
    trigger     TEXT    NOT NULL DEFAULT 'cron',   -- cron | manual
    log         TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX schedule_runs_schedule ON schedule_runs(schedule_id, id);

-- +goose Down
DROP TABLE schedule_runs;
DROP TABLE schedules;
