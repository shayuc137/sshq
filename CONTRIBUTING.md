# Contributing to sshq

Thanks for your interest in contributing. This guide covers the development setup and workflow.

## Prerequisites

- Go 1.23+
- An SSH key pair and at least one reachable SSH host (for integration testing)
- Git

## Setup

```bash
git clone https://github.com/shayuc137/sshq.git
cd sshq
go build ./...
go test ./...
```

## Project Structure

```
cmd/sshq/        — entry point
internal/
  cli/           — cobra command definitions
  config/        — SSH config parser + sshq metadata
  exec/          — remote command execution
  output/        — output layer (TTY detect, JSON/pretty)
  pool/          — connection pool daemon
  remote/        — shell detection, encoding, profile cache
  sshclient/     — SSH dial, ProxyJump, host key
  transfer/      — SFTP + raw stream file transfer
  tunnel/        — SSH tunnel management
skills/sshq/     — Claude Code skill package
docs/            — guides (en + zh-CN)
```

## Development Workflow

1. **Create a branch** from `main`.
2. **Make changes** — follow existing code patterns.
3. **Test** — `go test ./...` must pass. Add tests for new behavior.
4. **Lint** — `go vet ./...` must pass.
5. **Build** — `go build ./...` must pass.
6. **Commit** — use conventional commit messages (see below).
7. **Open a PR** against `main`.

## Commit Messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add cluster --hosts flag
fix: --env filter reads correct metadata key
refactor: unify daemon dispatch pattern
docs: add file transfer guide
test: add progress tracker duplicate test
chore: update dependencies
```

## Code Style

- **No comments unless they explain why**, not what.
- **No over-abstraction** — if a function is only called once, inline it.
- **Follow existing patterns** — check how similar features are implemented before adding new ones.
- **Output layer** — all user-facing output goes through `output.Writer`. Never write directly to `os.Stdout` from CLI commands.
- **Error handling** — return `output.Errorf(hint, action)` with an actionable suggestion.

## Documentation Sync

When modifying CLI commands (adding/removing commands, changing flags):

```bash
# Regenerate skill reference docs
go run ./cmd/sshq/ docs --skill skills/sshq/references/
```

If you add a top-level command, update the routing table in `skills/sshq/SKILL.md`.

## Testing

- Unit tests live next to the code they test (`*_test.go`).
- Tests that need SSH connections are guarded by build tags or skip conditions.
- Run the full suite: `go test ./...`
- Run a specific package: `go test ./internal/output/`

## Questions?

Open an issue or start a discussion.
