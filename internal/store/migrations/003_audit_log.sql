CREATE TABLE IF NOT EXISTS audit_log (
    id          TEXT PRIMARY KEY,
    timestamp   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    user_id     TEXT NOT NULL DEFAULT '',
    username    TEXT NOT NULL DEFAULT '',
    action      TEXT NOT NULL,
    target_type TEXT NOT NULL DEFAULT '',
    target_id   TEXT NOT NULL DEFAULT '',
    details     TEXT NOT NULL DEFAULT '',
    ip_address  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS audit_log_timestamp ON audit_log(timestamp);
CREATE INDEX IF NOT EXISTS audit_log_user_id   ON audit_log(user_id);
CREATE INDEX IF NOT EXISTS audit_log_action    ON audit_log(action);
INSERT OR IGNORE INTO schema_version (version) VALUES (3);
