-- +goose Up
ALTER TABLE users ADD COLUMN totp_backup_codes TEXT NOT NULL DEFAULT '[]';   -- JSON array of sha256 hex, consumed on use

-- +goose Down
ALTER TABLE users DROP COLUMN totp_backup_codes;
