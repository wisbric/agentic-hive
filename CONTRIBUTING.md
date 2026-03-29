# Contributing to Agentic Hive

Thank you for your interest in contributing!

## Development Setup

### Prerequisites

- Go 1.26+ with CGO enabled (required for SQLite)
- gcc / musl-dev (for SQLite C bindings)
- Node.js (optional, for xterm.js vendor updates)
- Docker (for container builds)
- Helm 3.x (for chart testing)

### Build from source

```bash
git clone https://github.com/your-org/agentic-hive.git
cd agentic-hive
go build -o agentic-hive ./cmd/server
```

### Run tests

```bash
go test ./... -v -race
go vet ./...
```

### Run locally

```bash
cp .env.example .env
# Edit .env with your settings
source .env
./agentic-hive
```

Open http://localhost:8080 and create your admin account.

## Making Changes

1. Fork the repository
2. Create a feature branch: `git checkout -b feat/my-feature`
3. Make your changes
4. Run tests: `go test ./... -race`
5. Commit with conventional commits: `feat:`, `fix:`, `docs:`, `refactor:`
6. Push and open a Pull Request

## Code Style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Keep files focused — one responsibility per file
- Use structured logging via `log/slog`
- Use typed constants from `store` package (roles, statuses)
- Add tests for new features

## Architecture

See [docs/architecture.md](docs/architecture.md) for the system design and [CLAUDE.md](CLAUDE.md) for AI agent instructions.

## Reporting Issues

Please use GitHub Issues. Include:
- What you expected
- What happened
- Steps to reproduce
- Go version and OS

## License

By contributing, you agree that your contributions will be licensed under the Apache License 2.0.
