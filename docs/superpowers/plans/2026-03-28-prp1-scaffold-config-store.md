# PRP-1: Project Scaffold, Config & SQLite Store

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create the Claude Overlay Go project with config loading, SQLite store with migrations, and health endpoints — producing a binary that starts an HTTP server and responds to `/healthz`.

**Architecture:** Single Go binary using `net/http` stdlib router. Config loaded from environment variables with sensible defaults. SQLite via `github.com/mattn/go-sqlite3` (CGO). Embedded SQL migrations run on startup.

**Tech Stack:** Go 1.26, SQLite3, `go:embed` for migrations and static files

**Spec:** `docs/superpowers/specs/2026-03-28-claude-overlay-design.md`

---

## File Structure

```
claude-overlay/
├── cmd/server/
│   └── main.go              — entry point: load config, open DB, run migrations, start HTTP server
├── internal/
│   ├── config/
│   │   ├── config.go        — Config struct + LoadConfig() from env vars
│   │   └── config_test.go   — tests for config loading
│   ├── store/
│   │   ├── store.go         — Open(), Migrate(), Close()
│   │   ├── store_test.go    — tests for migrations and CRUD
│   │   ├── models.go        — User, Server, SessionTemplate structs
│   │   ├── users.go         — CreateUser, GetUser, ListUsers, user count
│   │   ├── servers.go       — CreateServer, GetServer, ListServers, DeleteServer, UpdateServerStatus
│   │   ├── templates.go     — SeedTemplates, ListTemplates
│   │   └── migrations/
│   │       └── 001_initial.sql — tables: users, servers, ssh_keys, session_templates, schema_version
│   └── server/
│       ├── server.go        — HTTP server setup, routes, middleware
│       └── server_test.go   — tests for health endpoints
├── go.mod
└── go.sum
```

---

### Task 1: Initialize Go Module & Project Layout

**Files:**
- Create: `go.mod`
- Create: `cmd/server/main.go`

- [ ] **Step 1: Initialize git repo**

```bash
cd /home/stefans/git/agentic-workspace/projects/claude-overlay
git init
```

- [ ] **Step 2: Create go.mod**

```bash
go mod init gitlab.com/adfinisde/agentic-workspace/claude-overlay
```

- [ ] **Step 3: Create minimal main.go**

Create `cmd/server/main.go`:

```go
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println("claude-overlay starting")
	os.Exit(0)
}
```

- [ ] **Step 4: Verify it compiles and runs**

Run: `go build -o /tmp/claude-overlay ./cmd/server && /tmp/claude-overlay`
Expected: prints `claude-overlay starting` and exits 0

- [ ] **Step 5: Commit**

```bash
git add go.mod cmd/
git commit -m "feat: initialize Go module and project skeleton"
```

---

### Task 2: Config Loading

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

- [ ] **Step 1: Write failing tests for config loading**

Create `internal/config/config_test.go`:

```go
package config

import (
	"os"
	"testing"
)

func TestLoadConfigDefaults(t *testing.T) {
	// Clear any env vars that might interfere
	os.Unsetenv("OVERLAY_LISTEN")
	os.Unsetenv("OVERLAY_DB_PATH")
	os.Unsetenv("OVERLAY_AUTH_MODE")
	os.Unsetenv("OVERLAY_POLL_INTERVAL")
	os.Unsetenv("OVERLAY_IDLE_TIMEOUT")
	os.Unsetenv("OVERLAY_SESSION_SECRET")
	os.Unsetenv("OVERLAY_KEYSTORE_BACKEND")

	cfg := Load()

	if cfg.Listen != ":8080" {
		t.Errorf("Listen = %q, want %q", cfg.Listen, ":8080")
	}
	if cfg.DBPath != "/data/overlay.db" {
		t.Errorf("DBPath = %q, want %q", cfg.DBPath, "/data/overlay.db")
	}
	if cfg.AuthMode != "local" {
		t.Errorf("AuthMode = %q, want %q", cfg.AuthMode, "local")
	}
	if cfg.PollInterval != 30 {
		t.Errorf("PollInterval = %d, want %d", cfg.PollInterval, 30)
	}
	if cfg.IdleTimeout != 0 {
		t.Errorf("IdleTimeout = %d, want %d", cfg.IdleTimeout, 0)
	}
	if cfg.KeyStoreBackend != "local" {
		t.Errorf("KeyStoreBackend = %q, want %q", cfg.KeyStoreBackend, "local")
	}
}

func TestLoadConfigFromEnv(t *testing.T) {
	t.Setenv("OVERLAY_LISTEN", ":9090")
	t.Setenv("OVERLAY_DB_PATH", "/tmp/test.db")
	t.Setenv("OVERLAY_AUTH_MODE", "oidc")
	t.Setenv("OVERLAY_POLL_INTERVAL", "60")
	t.Setenv("OVERLAY_IDLE_TIMEOUT", "1800")
	t.Setenv("OVERLAY_SESSION_SECRET", "abc123")
	t.Setenv("OVERLAY_KEYSTORE_BACKEND", "vault")

	cfg := Load()

	if cfg.Listen != ":9090" {
		t.Errorf("Listen = %q, want %q", cfg.Listen, ":9090")
	}
	if cfg.DBPath != "/tmp/test.db" {
		t.Errorf("DBPath = %q, want %q", cfg.DBPath, "/tmp/test.db")
	}
	if cfg.AuthMode != "oidc" {
		t.Errorf("AuthMode = %q, want %q", cfg.AuthMode, "oidc")
	}
	if cfg.PollInterval != 60 {
		t.Errorf("PollInterval = %d, want %d", cfg.PollInterval, 60)
	}
	if cfg.IdleTimeout != 1800 {
		t.Errorf("IdleTimeout = %d, want %d", cfg.IdleTimeout, 1800)
	}
	if cfg.SessionSecret != "abc123" {
		t.Errorf("SessionSecret = %q, want %q", cfg.SessionSecret, "abc123")
	}
	if cfg.KeyStoreBackend != "vault" {
		t.Errorf("KeyStoreBackend = %q, want %q", cfg.KeyStoreBackend, "vault")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -v`
Expected: FAIL — `Load` not defined

- [ ] **Step 3: Implement config loading**

Create `internal/config/config.go`:

```go
package config

import (
	"os"
	"strconv"
)

type Config struct {
	Listen          string
	DBPath          string
	AuthMode        string // "local" or "oidc"
	SessionSecret   string
	PollInterval    int // seconds
	IdleTimeout     int // seconds, 0 = disabled
	KeyStoreBackend string // "local" or "vault"

	// OIDC (only when AuthMode == "oidc")
	OIDCIssuerURL  string
	OIDCClientID   string
	OIDCClientSecret string
	OIDCRedirectURL  string
	OIDCRolesClaim   string
	OIDCAdminGroup   string

	// Vault (only when KeyStoreBackend == "vault")
	VaultAddr       string
	VaultToken      string
	VaultSecretPath string

	// Emergency local auth when OIDC is primary
	EmergencyLocalAuth bool
}

func Load() *Config {
	return &Config{
		Listen:          envOr("OVERLAY_LISTEN", ":8080"),
		DBPath:          envOr("OVERLAY_DB_PATH", "/data/overlay.db"),
		AuthMode:        envOr("OVERLAY_AUTH_MODE", "local"),
		SessionSecret:   envOr("OVERLAY_SESSION_SECRET", ""),
		PollInterval:    envIntOr("OVERLAY_POLL_INTERVAL", 30),
		IdleTimeout:     envIntOr("OVERLAY_IDLE_TIMEOUT", 0),
		KeyStoreBackend: envOr("OVERLAY_KEYSTORE_BACKEND", "local"),

		OIDCIssuerURL:    envOr("OVERLAY_OIDC_ISSUER_URL", ""),
		OIDCClientID:     envOr("OVERLAY_OIDC_CLIENT_ID", ""),
		OIDCClientSecret: envOr("OVERLAY_OIDC_CLIENT_SECRET", ""),
		OIDCRedirectURL:  envOr("OVERLAY_OIDC_REDIRECT_URL", ""),
		OIDCRolesClaim:   envOr("OVERLAY_OIDC_ROLES_CLAIM", "groups"),
		OIDCAdminGroup:   envOr("OVERLAY_OIDC_ADMIN_GROUP", "overlay-admin"),

		VaultAddr:       envOr("OVERLAY_VAULT_ADDR", ""),
		VaultToken:      envOr("OVERLAY_VAULT_TOKEN", ""),
		VaultSecretPath: envOr("OVERLAY_VAULT_SECRET_PATH", "secret/claude-overlay/ssh-keys"),

		EmergencyLocalAuth: envOr("OVERLAY_EMERGENCY_LOCAL_AUTH", "") == "true",
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envIntOr(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: PASS (2/2 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/config/
git commit -m "feat: add config loading from environment variables"
```

---

### Task 3: SQLite Store — Setup & Migrations

**Files:**
- Create: `internal/store/store.go`
- Create: `internal/store/migrations/001_initial.sql`
- Create: `internal/store/store_test.go`

- [ ] **Step 1: Write failing test for store open + migrate**

Create `internal/store/store_test.go`:

```go
package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenAndMigrate(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open(%q) failed: %v", dbPath, err)
	}
	defer s.Close()

	// Verify the schema_version table exists and has version 1
	var version int
	err = s.db.QueryRow("SELECT version FROM schema_version ORDER BY version DESC LIMIT 1").Scan(&version)
	if err != nil {
		t.Fatalf("query schema_version: %v", err)
	}
	if version != 1 {
		t.Errorf("schema version = %d, want 1", version)
	}

	// Verify core tables exist by querying them
	tables := []string{"users", "servers", "ssh_keys", "session_templates"}
	for _, table := range tables {
		_, err := s.db.Exec("SELECT COUNT(*) FROM " + table)
		if err != nil {
			t.Errorf("table %q does not exist: %v", table, err)
		}
	}
}

func TestOpenCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "dir")
	dbPath := filepath.Join(dir, "test.db")

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open(%q) failed: %v", dbPath, err)
	}
	defer s.Close()

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Errorf("directory %q was not created", dir)
	}
}

func TestMigrateIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("first Open failed: %v", err)
	}
	s.Close()

	// Open again — migrations should not fail
	s2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("second Open failed: %v", err)
	}
	defer s2.Close()
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -v`
Expected: FAIL — `Open` not defined

- [ ] **Step 3: Create the initial migration SQL**

Create `internal/store/migrations/001_initial.sql`:

```sql
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER PRIMARY KEY,
    applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    username TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL DEFAULT 'user',
    oidc_subject TEXT UNIQUE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS servers (
    id TEXT PRIMARY KEY,
    name TEXT UNIQUE NOT NULL,
    host TEXT NOT NULL,
    port INTEGER NOT NULL DEFAULT 22,
    ssh_user TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'unknown',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS ssh_keys (
    server_id TEXT PRIMARY KEY REFERENCES servers(id) ON DELETE CASCADE,
    encrypted_key BLOB NOT NULL,
    salt BLOB NOT NULL,
    nonce BLOB NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS session_templates (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    command TEXT NOT NULL,
    workdir TEXT NOT NULL DEFAULT '~/',
    server_id TEXT REFERENCES servers(id) ON DELETE CASCADE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

INSERT OR IGNORE INTO schema_version (version) VALUES (1);
```

- [ ] **Step 4: Implement store.Open with embedded migrations**

Create `internal/store/store.go`:

```go
package store

import (
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type Store struct {
	db *sql.DB
}

func Open(dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o750); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) migrate() error {
	// Get current version
	var currentVersion int
	_ = s.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&currentVersion)

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// Parse version from filename: "001_initial.sql" -> 1
		var version int
		if _, err := fmt.Sscanf(entry.Name(), "%d_", &version); err != nil {
			continue
		}

		if version <= currentVersion {
			continue
		}

		content, err := migrationsFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}

		if _, err := s.db.Exec(string(content)); err != nil {
			return fmt.Errorf("execute migration %s: %w", entry.Name(), err)
		}
	}

	return nil
}
```

- [ ] **Step 5: Add go-sqlite3 dependency**

Run: `go mod tidy`

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/store/ -v`
Expected: PASS (3/3 tests)

- [ ] **Step 7: Commit**

```bash
git add internal/store/store.go internal/store/store_test.go internal/store/migrations/ go.mod go.sum
git commit -m "feat: add SQLite store with embedded migrations"
```

---

### Task 4: Store Models & User CRUD

**Files:**
- Create: `internal/store/models.go`
- Create: `internal/store/users.go`
- Modify: `internal/store/store_test.go`

- [ ] **Step 1: Write failing tests for user CRUD**

Add to `internal/store/store_test.go`:

```go
func testStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCreateAndGetUser(t *testing.T) {
	s := testStore(t)

	user, err := s.CreateUser("admin", "hashedpw", "admin")
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if user.Username != "admin" {
		t.Errorf("Username = %q, want %q", user.Username, "admin")
	}
	if user.Role != "admin" {
		t.Errorf("Role = %q, want %q", user.Role, "admin")
	}
	if user.ID == "" {
		t.Error("ID should not be empty")
	}

	got, err := s.GetUserByUsername("admin")
	if err != nil {
		t.Fatalf("GetUserByUsername failed: %v", err)
	}
	if got.ID != user.ID {
		t.Errorf("got ID = %q, want %q", got.ID, user.ID)
	}
	if got.PasswordHash != "hashedpw" {
		t.Errorf("PasswordHash = %q, want %q", got.PasswordHash, "hashedpw")
	}
}

func TestCreateUserDuplicate(t *testing.T) {
	s := testStore(t)

	_, err := s.CreateUser("admin", "pw1", "admin")
	if err != nil {
		t.Fatalf("first CreateUser failed: %v", err)
	}

	_, err = s.CreateUser("admin", "pw2", "admin")
	if err == nil {
		t.Error("expected error for duplicate username, got nil")
	}
}

func TestUserCount(t *testing.T) {
	s := testStore(t)

	count, err := s.UserCount()
	if err != nil {
		t.Fatalf("UserCount failed: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}

	_, _ = s.CreateUser("u1", "pw", "user")
	_, _ = s.CreateUser("u2", "pw", "admin")

	count, err = s.UserCount()
	if err != nil {
		t.Fatalf("UserCount failed: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/ -v -run 'TestCreate|TestUser'`
Expected: FAIL — `CreateUser`, `GetUserByUsername`, `UserCount` not defined

- [ ] **Step 3: Create models**

Create `internal/store/models.go`:

```go
package store

import "time"

type User struct {
	ID           string
	Username     string
	PasswordHash string
	Role         string // "admin" or "user"
	OIDCSubject  *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Server struct {
	ID        string
	Name      string
	Host      string
	Port      int
	SSHUser   string
	Status    string // "unknown", "reachable", "unreachable"
	CreatedAt time.Time
	UpdatedAt time.Time
}

type SessionTemplate struct {
	ID        string
	Name      string
	Command   string
	Workdir   string
	ServerID  *string // nil = global
	CreatedAt time.Time
}
```

- [ ] **Step 4: Implement user CRUD**

Create `internal/store/users.go`:

```go
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

func (s *Store) GetUserByUsername(username string) (*User, error) {
	u := &User{}
	err := s.db.QueryRow(
		"SELECT id, username, password_hash, role, oidc_subject, created_at, updated_at FROM users WHERE username = ?",
		username,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.OIDCSubject, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user not found: %s", username)
	}
	if err != nil {
		return nil, fmt.Errorf("query user: %w", err)
	}
	return u, nil
}

func (s *Store) GetUserByID(id string) (*User, error) {
	u := &User{}
	err := s.db.QueryRow(
		"SELECT id, username, password_hash, role, oidc_subject, created_at, updated_at FROM users WHERE id = ?",
		id,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.Role, &u.OIDCSubject, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("query user: %w", err)
	}
	return u, nil
}

func (s *Store) UserCount() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	return count, err
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/store/ -v -run 'TestCreate|TestUser'`
Expected: PASS (3/3 new tests)

- [ ] **Step 6: Commit**

```bash
git add internal/store/models.go internal/store/users.go internal/store/store_test.go
git commit -m "feat: add store models and user CRUD operations"
```

---

### Task 5: Store — Server CRUD & Templates

**Files:**
- Create: `internal/store/servers.go`
- Create: `internal/store/templates.go`
- Modify: `internal/store/store_test.go`

- [ ] **Step 1: Write failing tests for server CRUD**

Add to `internal/store/store_test.go`:

```go
func TestCreateAndGetServer(t *testing.T) {
	s := testStore(t)

	srv, err := s.CreateServer("devbox", "devbox.wisbric.com", 22, "stefan")
	if err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}
	if srv.Name != "devbox" {
		t.Errorf("Name = %q, want %q", srv.Name, "devbox")
	}
	if srv.Status != "unknown" {
		t.Errorf("Status = %q, want %q", srv.Status, "unknown")
	}

	got, err := s.GetServer(srv.ID)
	if err != nil {
		t.Fatalf("GetServer failed: %v", err)
	}
	if got.Host != "devbox.wisbric.com" {
		t.Errorf("Host = %q, want %q", got.Host, "devbox.wisbric.com")
	}
}

func TestListServers(t *testing.T) {
	s := testStore(t)

	_, _ = s.CreateServer("a", "a.example.com", 22, "root")
	_, _ = s.CreateServer("b", "b.example.com", 2222, "deploy")

	servers, err := s.ListServers()
	if err != nil {
		t.Fatalf("ListServers failed: %v", err)
	}
	if len(servers) != 2 {
		t.Errorf("len = %d, want 2", len(servers))
	}
}

func TestDeleteServer(t *testing.T) {
	s := testStore(t)

	srv, _ := s.CreateServer("del", "del.example.com", 22, "root")
	if err := s.DeleteServer(srv.ID); err != nil {
		t.Fatalf("DeleteServer failed: %v", err)
	}

	servers, _ := s.ListServers()
	if len(servers) != 0 {
		t.Errorf("len = %d, want 0 after delete", len(servers))
	}
}

func TestUpdateServerStatus(t *testing.T) {
	s := testStore(t)

	srv, _ := s.CreateServer("s1", "s1.example.com", 22, "root")
	if err := s.UpdateServerStatus(srv.ID, "reachable"); err != nil {
		t.Fatalf("UpdateServerStatus failed: %v", err)
	}

	got, _ := s.GetServer(srv.ID)
	if got.Status != "reachable" {
		t.Errorf("Status = %q, want %q", got.Status, "reachable")
	}
}
```

- [ ] **Step 2: Write failing tests for templates**

Add to `internal/store/store_test.go`:

```go
func TestSeedAndListTemplates(t *testing.T) {
	s := testStore(t)

	if err := s.SeedTemplates(); err != nil {
		t.Fatalf("SeedTemplates failed: %v", err)
	}

	templates, err := s.ListTemplates("")
	if err != nil {
		t.Fatalf("ListTemplates failed: %v", err)
	}
	if len(templates) < 3 {
		t.Errorf("expected at least 3 default templates, got %d", len(templates))
	}

	// Seed again should be idempotent
	if err := s.SeedTemplates(); err != nil {
		t.Fatalf("second SeedTemplates failed: %v", err)
	}

	templates2, _ := s.ListTemplates("")
	if len(templates2) != len(templates) {
		t.Errorf("template count changed after re-seed: %d -> %d", len(templates), len(templates2))
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/store/ -v -run 'TestCreate.*Server|TestList.*Server|TestDelete|TestUpdate.*Server|TestSeed'`
Expected: FAIL — functions not defined

- [ ] **Step 4: Implement server CRUD**

Create `internal/store/servers.go`:

```go
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
		Status:  "unknown",
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
```

- [ ] **Step 5: Implement template seeding**

Create `internal/store/templates.go`:

```go
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
```

- [ ] **Step 6: Run all store tests**

Run: `go test ./internal/store/ -v`
Expected: PASS (all tests)

- [ ] **Step 7: Commit**

```bash
git add internal/store/servers.go internal/store/templates.go internal/store/store_test.go
git commit -m "feat: add server CRUD and session template seeding"
```

---

### Task 6: HTTP Server with Health Endpoints

**Files:**
- Create: `internal/server/server.go`
- Create: `internal/server/server_test.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Write failing tests for health endpoints**

Create `internal/server/server_test.go`:

```go
package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"gitlab.com/adfinisde/agentic-workspace/claude-overlay/internal/config"
	"gitlab.com/adfinisde/agentic-workspace/claude-overlay/internal/store"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open failed: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	cfg := &config.Config{
		Listen:    ":0",
		AuthMode:  "local",
	}

	srv := New(cfg, st)
	return srv
}

func TestHealthz(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if w.Body.String() != `{"status":"ok"}` {
		t.Errorf("body = %q, want %q", w.Body.String(), `{"status":"ok"}`)
	}
}

func TestReadyz(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/server/ -v`
Expected: FAIL — `server` package not found

- [ ] **Step 3: Implement HTTP server**

Create `internal/server/server.go`:

```go
package server

import (
	"encoding/json"
	"log"
	"net/http"

	"gitlab.com/adfinisde/agentic-workspace/claude-overlay/internal/config"
	"gitlab.com/adfinisde/agentic-workspace/claude-overlay/internal/store"
)

type Server struct {
	cfg   *config.Config
	store *store.Store
	mux   *http.ServeMux
}

func New(cfg *config.Config, st *store.Store) *Server {
	s := &Server{
		cfg:   cfg,
		store: st,
		mux:   http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) ListenAndServe() error {
	log.Printf("listening on %s", s.cfg.Listen)
	return http.ListenAndServe(s.cfg.Listen, s.mux)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.HandleFunc("GET /readyz", s.handleReadyz)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	// For now: always ready. Later PRPs will check server reachability.
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/server/ -v`
Expected: PASS (2/2 tests)

- [ ] **Step 5: Commit**

```bash
git add internal/server/
git commit -m "feat: add HTTP server with healthz and readyz endpoints"
```

---

### Task 7: Wire Everything in main.go

**Files:**
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Update main.go to wire config, store, and server**

Replace `cmd/server/main.go` with:

```go
package main

import (
	"log"

	"gitlab.com/adfinisde/agentic-workspace/claude-overlay/internal/config"
	"gitlab.com/adfinisde/agentic-workspace/claude-overlay/internal/server"
	"gitlab.com/adfinisde/agentic-workspace/claude-overlay/internal/store"
)

func main() {
	cfg := config.Load()

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer st.Close()

	if err := st.SeedTemplates(); err != nil {
		log.Fatalf("failed to seed templates: %v", err)
	}

	log.Printf("claude-overlay starting (auth=%s, keystore=%s)", cfg.AuthMode, cfg.KeyStoreBackend)

	srv := server.New(cfg, st)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
```

- [ ] **Step 2: Run go mod tidy**

Run: `go mod tidy`

- [ ] **Step 3: Build and verify**

Run: `go build -o /tmp/claude-overlay ./cmd/server`
Expected: builds successfully

- [ ] **Step 4: Run the binary and test health endpoint**

Run in background:
```bash
OVERLAY_DB_PATH=/tmp/overlay-test.db OVERLAY_LISTEN=:18080 /tmp/claude-overlay &
sleep 1
curl -s http://localhost:18080/healthz
curl -s http://localhost:18080/readyz
kill %1
rm /tmp/overlay-test.db
```
Expected:
```
{"status":"ok"}
{"status":"ok"}
```

- [ ] **Step 5: Run all tests**

Run: `go test ./... -v`
Expected: PASS (all packages)

- [ ] **Step 6: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat: wire config, store, and HTTP server in main"
```

---

### Task 8: Add .gitignore and Final Cleanup

**Files:**
- Create: `.gitignore`

- [ ] **Step 1: Create .gitignore**

Create `.gitignore`:

```
# Go
/claude-overlay
*.exe
*.test
*.out

# SQLite
*.db
*.db-wal
*.db-shm

# IDE
.idea/
.vscode/
*.swp
*.swo

# OS
.DS_Store
Thumbs.db

# Build
/tmp/
/dist/
```

- [ ] **Step 2: Verify full test suite passes**

Run: `go test ./... -v`
Expected: PASS (all packages)

- [ ] **Step 3: Verify build**

Run: `go build -o /tmp/claude-overlay ./cmd/server && echo "BUILD OK"`
Expected: `BUILD OK`

- [ ] **Step 4: Commit**

```bash
git add .gitignore
git commit -m "chore: add .gitignore"
```

---

## Verification Summary

After all tasks complete, the following must pass:

| Check | Command | Expected |
|-------|---------|----------|
| Build | `go build -o /tmp/claude-overlay ./cmd/server` | exit 0 |
| All tests | `go test ./... -v` | PASS (all) |
| Healthz | start server, `curl localhost:18080/healthz` | `{"status":"ok"}` |
| Readyz | `curl localhost:18080/readyz` | `{"status":"ok"}` |
| DB created | `ls /tmp/overlay-test.db` after server start | file exists |
| Migrations | `sqlite3 /tmp/overlay-test.db ".tables"` | lists users, servers, ssh_keys, session_templates, schema_version |
| Templates seeded | `sqlite3 /tmp/overlay-test.db "SELECT name FROM session_templates"` | Claude Code, Claude Code (resume), Shell |
