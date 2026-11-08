-- +goose Up
CREATE INDEX audit_log_target ON audit_log(target, id);

-- +goose Down
DROP INDEX audit_log_target;
