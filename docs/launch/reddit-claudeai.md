# r/ClaudeAI Post

**Title:** I built a web dashboard to manage Claude Code sessions across multiple servers — open source

**Body:**

I've been running Claude Code on multiple dev servers via tmux and got tired of SSH-ing between them to check on tasks, create new sessions, and manage everything.

So I built **Agentic Hive** — a lightweight web app that connects to your servers via SSH and lets you manage tmux sessions from a single dashboard.

**What it does:**

- Register your SSH-accessible servers (devbox, staging, etc.)
- Create sessions from templates: Claude Code, Claude Code (full access), Codex, Shell
- Open any session in a browser terminal (xterm.js) or copy the SSH command
- Per-user isolation — each user manages their own servers
- OIDC/Keycloak SSO for team use

**Why I built it:**

I'm running 10+ concurrent Claude Code sessions across two servers. Before this, I had a dozen terminal tabs with SSH connections and would constantly lose track of which session was doing what. Now I just open the dashboard, see everything at a glance, and click into whatever needs attention.

The browser terminal is surprisingly good — I leave tabs open to different sessions and switch between them like browser tabs instead of terminal tabs.

**Try it:**

```bash
docker run -p 8080:8080 \
  -e OVERLAY_SESSION_SECRET=$(openssl rand -hex 32) \
  -v hive-data:/data \
  ghcr.io/wisbric/agentic-hive:latest
```

GitHub: https://github.com/wisbric/agentic-hive

Single Go binary, no external dependencies, Apache 2.0 licensed. Also supports Codex sessions if you have that set up.

Screenshots in the README. Happy to answer questions!
