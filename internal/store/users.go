package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
)

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Store) CreateUser(username, passwordHash, role string) (*User, error) {
	u := &User{
		ID:           newID(),
		Username:     username,
		PasswordHash: passwordHash,
		Role:         role,
	}

	_, err := s.db.Exec(
		"INSERT INTO users (id, username, password_hash, role) VALUES (?, ?, ?, ?)",
		u.ID, u.Username, u.PasswordHash, u.Role,
	)
	if err != nil {
		return nil, fmt.Errorf("insert user: %w", err)
	}

	return u, nil
}

func scanUser(row *sql.Row) (*User, error) {
	u := &User{}
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.OIDCSubject, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user not found")
	}
	if err != nil {
		return nil, fmt.Errorf("query user: %w", err)
	}
	return u, nil
}

func (s *Store) GetUserByUsername(username string) (*User, error) {
	return scanUser(s.db.QueryRow(
		"SELECT id, username, password_hash, role, oidc_subject, created_at, updated_at FROM users WHERE username = ?",
		username,
	))
}

func (s *Store) GetUserByID(id string) (*User, error) {
	return scanUser(s.db.QueryRow(
		"SELECT id, username, password_hash, role, oidc_subject, created_at, updated_at FROM users WHERE id = ?",
		id,
	))
}

func (s *Store) UserCount() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	return count, err
}

func (s *Store) ListUsers() ([]User, error) {
	rows, err := s.db.Query(
		"SELECT id, username, password_hash, role, oidc_subject, created_at, updated_at FROM users ORDER BY created_at ASC",
	)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.OIDCSubject, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *Store) DeleteUser(id string) error {
	res, err := s.db.Exec("DELETE FROM users WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("user not found")
	}
	return nil
}

func (s *Store) UpsertOIDCUser(oidcSubject, username, role string) (*User, error) {
	u := &User{}
	err := s.db.QueryRow(
		"SELECT id, username, password_hash, role, oidc_subject, created_at, updated_at FROM users WHERE oidc_subject = ?",
		oidcSubject,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.OIDCSubject, &u.CreatedAt, &u.UpdatedAt)

	if err == nil {
		_, err = s.db.Exec(
			"UPDATE users SET username = ?, role = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
			username, role, u.ID,
		)
		if err != nil {
			return nil, fmt.Errorf("update oidc user: %w", err)
		}
		u.Username = username
		u.Role = role
		return u, nil
	}

	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("query oidc user: %w", err)
	}

	u = &User{
		ID:          newID(),
		Username:    username,
		Role:        role,
		OIDCSubject: &oidcSubject,
	}

	_, err = s.db.Exec(
		"INSERT INTO users (id, username, role, oidc_subject) VALUES (?, ?, ?, ?)",
		u.ID, u.Username, u.Role, u.OIDCSubject,
	)
	if err != nil {
		return nil, fmt.Errorf("insert oidc user: %w", err)
	}

	return u, nil
}
