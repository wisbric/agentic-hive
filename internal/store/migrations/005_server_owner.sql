ALTER TABLE servers ADD COLUMN owner_id TEXT NOT NULL DEFAULT '';

INSERT OR IGNORE INTO schema_version (version) VALUES (5);
