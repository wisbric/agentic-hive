# Show HN: Agentic Hive – Manage Claude Code and Codex tmux sessions from a browser

**URL:** https://github.com/wisbric/agentic-hive

**Text:**

I built Agentic Hive because I was running Claude Code and Codex sessions across multiple dev servers and constantly SSH-ing between them to check on long-running tasks. I wanted one place to see all my sessions, create new ones, and jump into any terminal from a browser tab.

It's a single Go binary that connects to your SSH-accessible hosts and manages tmux sessions on them. Two ways to connect: click "Terminal" for an in-browser xterm.js terminal, or click "SSH" to copy the exact ssh command to run in your own terminal.

Key features:

- Session templates for Claude Code, Claude Code (full access/--dangerously-skip-permissions), Codex, Shell, or custom commands
- Per-user isolation — each user manages their own servers. Admins see everything.
- OIDC/Keycloak SSO or local auth with bcrypt
- SSH keys encrypted at rest (AES-256-GCM) or stored in HashiCorp Vault/OpenBao — users can reference existing keys in Vault instead of pasting them
- Helm chart for Kubernetes, docker-compose for standalone, or just run the binary
- No external dependencies — SQLite for state, everything in one ~25MB binary

Tech: Go 1.26, xterm.js, WebSocket, vanilla JS (no build step), SQLite, Helm.

I'm using it to manage about a dozen concurrent Claude Code sessions across two dev servers. The browser terminal is surprisingly usable — I often just leave tabs open to different sessions and switch between them.

Happy to answer questions about the architecture or the Go WebSocket↔SSH bridge implementation.
