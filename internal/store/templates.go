package store

import "fmt"

var defaultTemplates = []SessionTemplate{
	{Name: "Claude Code", Command: "claude", Workdir: "~/"},
	{Name: "Claude Code (resume)", Command: "claude --resume", Workdir: "~/"},
	{Name: "Shell", Command: "bash", Workdir: "~/"},
}

func (s *Store) SeedTemplates() error {
	for _, tmpl := range defaultTemplates {
		var count int
		err := s.db.QueryRow("SELECT COUNT(*) FROM session_templates WHERE name = ? AND server_id IS NULL", tmpl.Name).Scan(&count)
		if err != nil {
			return fmt.Errorf("check template %q: %w", tmpl.Name, err)
		}
		if count > 0 {
			continue
		}
		_, err = s.db.Exec(
			"INSERT INTO session_templates (id, name, command, workdir) VALUES (?, ?, ?, ?)",
			newID(), tmpl.Name, tmpl.Command, tmpl.Workdir,
		)
		if err != nil {
			return fmt.Errorf("insert template %q: %w", tmpl.Name, err)
		}
	}
	return nil
}

func (s *Store) ListTemplates(serverID string) ([]SessionTemplate, error) {
	query := "SELECT id, name, command, workdir, server_id, created_at FROM session_templates WHERE server_id IS NULL"
	args := []any{}

	if serverID != "" {
		query = "SELECT id, name, command, workdir, server_id, created_at FROM session_templates WHERE server_id IS NULL OR server_id = ? ORDER BY name"
		args = append(args, serverID)
	} else {
		query += " ORDER BY name"
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query templates: %w", err)
	}
	defer rows.Close()

	var templates []SessionTemplate
	for rows.Next() {
		var t SessionTemplate
		if err := rows.Scan(&t.ID, &t.Name, &t.Command, &t.Workdir, &t.ServerID, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan template: %w", err)
		}
		templates = append(templates, t)
	}
	return templates, rows.Err()
}
