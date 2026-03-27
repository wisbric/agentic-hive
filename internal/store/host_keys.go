package store

import (
	"database/sql"
	"fmt"
)

// StoreHostKey upserts the host key and fingerprint for the given server.
func (s *Store) StoreHostKey(serverID string, hostKey []byte, fingerprint string) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO host_keys (server_id, host_key, fingerprint) VALUES (?, ?, ?)`,
		serverID, hostKey, fingerprint,
	)
	return err
}

// GetHostKey returns the stored host key and fingerprint for the given server.
// Returns an error wrapping "no host key" when no row exists.
func (s *Store) GetHostKey(serverID string) (hostKey []byte, fingerprint string, err error) {
	row := s.db.QueryRow(
		`SELECT host_key, fingerprint FROM host_keys WHERE server_id = ?`,
		serverID,
	)
	if err = row.Scan(&hostKey, &fingerprint); err != nil {
		if err == sql.ErrNoRows {
			return nil, "", fmt.Errorf("no host key")
		}
		return nil, "", err
	}
	return hostKey, fingerprint, nil
}

// DeleteHostKey removes the stored host key for the given server.
// Does not return an error if no row exists.
func (s *Store) DeleteHostKey(serverID string) error {
	_, err := s.db.Exec(`DELETE FROM host_keys WHERE server_id = ?`, serverID)
	return err
}
