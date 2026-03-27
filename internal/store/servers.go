package store

import (
	"database/sql"
	"fmt"
)

func (s *Store) CreateServer(name, host string, port int, sshUser string) (*Server, error) {
	srv := &Server{
		ID:      newID(),
		Name:    name,
		Host:    host,
		Port:    port,
		SSHUser: sshUser,
		Status:  StatusUnknown,
	}

	_, err := s.db.Exec(
		"INSERT INTO servers (id, name, host, port, ssh_user, status) VALUES (?, ?, ?, ?, ?, ?)",
		srv.ID, srv.Name, srv.Host, srv.Port, srv.SSHUser, srv.Status,
	)
	if err != nil {
		return nil, fmt.Errorf("insert server: %w", err)
	}

	return srv, nil
}

func (s *Store) GetServer(id string) (*Server, error) {
	srv := &Server{}
	err := s.db.QueryRow(
		"SELECT id, name, host, port, ssh_user, status, created_at, updated_at FROM servers WHERE id = ?",
		id,
	).Scan(&srv.ID, &srv.Name, &srv.Host, &srv.Port, &srv.SSHUser, &srv.Status, &srv.CreatedAt, &srv.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("server not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("query server: %w", err)
	}
	return srv, nil
}

func (s *Store) ListServers() ([]Server, error) {
	rows, err := s.db.Query("SELECT id, name, host, port, ssh_user, status, created_at, updated_at FROM servers ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("query servers: %w", err)
	}
	defer rows.Close()

	var servers []Server
	for rows.Next() {
		var srv Server
		if err := rows.Scan(&srv.ID, &srv.Name, &srv.Host, &srv.Port, &srv.SSHUser, &srv.Status, &srv.CreatedAt, &srv.UpdatedAt); err != nil {
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
