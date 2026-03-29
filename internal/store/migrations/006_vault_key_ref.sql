ALTER TABLE servers ADD COLUMN key_source TEXT NOT NULL DEFAULT 'local';
ALTER TABLE servers ADD COLUMN vault_key_path TEXT NOT NULL DEFAULT '';

INSERT OR IGNORE INTO schema_version (version) VALUES (6);
