-- +goose Up
ALTER TABLE users ADD COLUMN totp_backup_codes TEXT NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE users DROP COLUMN totp_backup_codes;
