package store

import "time"

type AuditEntry struct {
	ID         string
	Timestamp  time.Time
	UserID     string
	Username   string
	Action     string
	TargetType string
	TargetID   string
	Details    string
	IPAddress  string
}

const (
	RoleAdmin = "admin"
	RoleUser  = "user"

	StatusUnknown     = "unknown"
	StatusReachable   = "reachable"
	StatusUnreachable = "unreachable"
	StatusKeyMismatch = "key_mismatch"
)

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
	OwnerID   string
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
