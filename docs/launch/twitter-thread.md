# Twitter/X Thread

**Tweet 1 (hook):**

I was running 10+ Claude Code sessions across multiple servers and losing track of everything.

So I built Agentic Hive — a web dashboard to manage all your AI coding sessions from one place.

Open source, single binary, deploys in 30 seconds.

🔗 github.com/wisbric/agentic-hive

🧵👇

**Tweet 2 (the problem):**

The problem: you SSH into devbox, start `claude`, start another session, SSH into staging, start another...

Soon you have 15 terminal tabs and no idea which session is where or what it's doing.

**Tweet 3 (the solution):**

Agentic Hive connects to your servers via SSH and manages tmux sessions.

Two ways to connect:
• Browser terminal (xterm.js) — click and you're in
• SSH command copy — for your local terminal

Works with Claude Code, Codex, or any CLI tool.

[attach dashboard screenshot]

**Tweet 4 (team features):**

Built for teams:
• OIDC/Keycloak SSO
• Per-user server isolation
• SSH keys in Vault/OpenBao (reference existing keys, live read)
• Audit log
• Admin UI for config (no restart needed)

**Tweet 5 (tech):**

Tech details for the curious:
• Single Go binary (~25MB), SQLite, zero external deps
• WebSocket ↔ SSH PTY bridge for browser terminal
• AES-256-GCM encrypted SSH keys (Argon2id KDF)
• Prometheus metrics, structured logging, graceful shutdown
• Helm chart + docker-compose

**Tweet 6 (CTA):**

Try it:

```
docker run -p 8080:8080 \
  -e OVERLAY_SESSION_SECRET=$(openssl rand -hex 32) \
  ghcr.io/wisbric/agentic-hive:latest
```

Open localhost:8080, create admin account, add a server, start coding.

Apache 2.0 licensed. Contributions welcome.

github.com/wisbric/agentic-hive
