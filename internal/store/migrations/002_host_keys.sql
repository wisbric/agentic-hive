CREATE TABLE IF NOT EXISTS host_keys (
    server_id  TEXT PRIMARY KEY REFERENCES servers(id) ON DELETE CASCADE,
    host_key   BLOB NOT NULL,
    fingerprint TEXT NOT NULL,
    first_seen DATETIME DEFAULT CURRENT_TIMESTAMP
);
INSERT OR IGNORE INTO schema_version (version) VALUES (2);
