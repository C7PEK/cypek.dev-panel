-- +goose Up
ALTER TABLE servers ADD COLUMN install_state TEXT NOT NULL DEFAULT 'ready'
    CHECK (install_state IN ('pending', 'installing', 'ready', 'failed'));
ALTER TABLE servers ADD COLUMN install_error TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE servers DROP COLUMN install_error;
ALTER TABLE servers DROP COLUMN install_state;
