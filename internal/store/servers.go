package store

import (
	"database/sql"
	"fmt"
)

func (s *Store) CreateServer(name, host string, port int, sshUser, ownerID, keySource, vaultKeyPath string) (*Server, error) {
	if keySource == "" {
		keySource = "local"
	}
	srv := &Server{
		ID:           newID(),
		Name:         name,
		Host:         host,
		Port:         port,
		SSHUser:      sshUser,
		Status:       StatusUnknown,
		OwnerID:      ownerID,
		KeySource:    keySource,
		VaultKeyPath: vaultKeyPath,
	}

	_, err := s.db.Exec(
		"INSERT INTO servers (id, name, host, port, ssh_user, status, owner_id, key_source, vault_key_path) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		srv.ID, srv.Name, srv.Host, srv.Port, srv.SSHUser, srv.Status, srv.OwnerID, srv.KeySource, srv.VaultKeyPath,
	)
	if err != nil {
		return nil, fmt.Errorf("insert server: %w", err)
	}

	return srv, nil
}

// GetServer returns a server by ID. If ownerID is non-empty, it verifies the server
// belongs to that user or is unowned (owner_id = ''). Admins pass ownerID = "".
func (s *Store) GetServer(id string, ownerID string) (*Server, error) {
	srv := &Server{}
	var query string
	var args []any

	if ownerID == "" {
		query = "SELECT id, name, host, port, ssh_user, status, owner_id, key_source, vault_key_path, created_at, updated_at FROM servers WHERE id = ?"
		args = []any{id}
	} else {
		query = "SELECT id, name, host, port, ssh_user, status, owner_id, key_source, vault_key_path, created_at, updated_at FROM servers WHERE id = ? AND (owner_id = ? OR owner_id = '')"
		args = []any{id, ownerID}
	}

	err := s.db.QueryRow(query, args...).Scan(
		&srv.ID, &srv.Name, &srv.Host, &srv.Port, &srv.SSHUser, &srv.Status, &srv.OwnerID, &srv.KeySource, &srv.VaultKeyPath, &srv.CreatedAt, &srv.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("server not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("query server: %w", err)
	}
	return srv, nil
}

// ListServers returns servers. If ownerID is empty (admin), all servers are returned.
// If ownerID is set, only servers owned by that user or unowned (legacy) servers are returned.
func (s *Store) ListServers(ownerID string) ([]Server, error) {
	var rows *sql.Rows
	var err error

	if ownerID == "" {
		rows, err = s.db.Query("SELECT id, name, host, port, ssh_user, status, owner_id, key_source, vault_key_path, created_at, updated_at FROM servers ORDER BY name")
	} else {
		rows, err = s.db.Query(
			"SELECT id, name, host, port, ssh_user, status, owner_id, key_source, vault_key_path, created_at, updated_at FROM servers WHERE owner_id = ? OR owner_id = '' ORDER BY name",
			ownerID,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("query servers: %w", err)
	}
	defer rows.Close()

	var servers []Server
	for rows.Next() {
		var srv Server
		if err := rows.Scan(&srv.ID, &srv.Name, &srv.Host, &srv.Port, &srv.SSHUser, &srv.Status, &srv.OwnerID, &srv.KeySource, &srv.VaultKeyPath, &srv.CreatedAt, &srv.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan server: %w", err)
		}
		servers = append(servers, srv)
	}
	return servers, rows.Err()
}

func (s *Store) DeleteServer(id string) error {
	_, err := s.db.Exec("DELETE FROM servers WHERE id = ?", id)
	return err
}

func (s *Store) UpdateServerStatus(id, status string) error {
	_, err := s.db.Exec("UPDATE servers SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?", status, id)
	return err
}
