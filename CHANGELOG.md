# Changelog

## v0.2.0 (2026-07-02)

### Security layer

- **Encrypted credential store**: passwords encrypted with age using SSH public key, stored in OS config directory. `sshq credential set/list/delete`. No command prints stored passwords.
- **Capability policy**: command whitelist/blacklist, local/remote path whitelist, per-host override/append mode, all configured in `config.toml`. `sshq policy validate/check/list/grant/revoke`.
- **Tunnel forward whitelist**: `local_forward_whitelist` / `remote_forward_whitelist` restrict which targets tunnels can reach. Matching supports exact, port wildcard, port range, and host wildcard.
- **Temporary grants**: `sshq policy grant` with TTL, requires controlling terminal (agents cannot self-grant), never overrides blacklists.
- **Audit logging**: JSONL metadata for exec, cp, tunnel, cluster, and policy-blocked operations. SHA-256 hash for script-file. Rotation at configurable max size. `sshq audit` query CLI. Fail-closed when log is unwritable.

### New commands

- `sshq docs --verify <dir>`: detect drift between cobra command tree and generated skill reference docs. Exit 0 when consistent, exit 1 with diff summary.
- `sshq policy check --local-forward/--remote-forward`: test tunnel forward policy decisions.

### CLI improvements

- `sshq skill install` simplified: `--target`/`--scope` replaced with `--codex`/`--project` boolean flags.
- Connecting status moved to verbose output — stderr is silent by default for direct connections.

### Documentation

- README rewritten with problem-driven intro, TTY before/after demo, security model section, and platform-specific install instructions (no Go required).
- All 8 bilingual guides updated with policy/audit coverage.
- SKILL.md restructured: environment checks, safety confirmations, expanded routing table, JSON-first error handling.
- Reference generator enhanced with output fields, exit behavior, agent handling notes, and security annotations per command group.
- Bilingual (English + Chinese) guides fixed: broken links, stale titles, over-backticked terms.

## v0.1.0 (2026-06-28)

Initial release.

- exec with TTY auto-detect (pipe → JSON, terminal → pretty), stdout purity guarantee
- cp with SFTP + raw byte stream fallback, server-to-server relay
- daemon connection pool with graceful degradation
- cross-shell detection (bash/ash/powershell/cmd) and automatic command wrapping
- cluster concurrent execution with tag/env/hosts filtering
- local/remote SSH tunnels with exponential backoff reconnect
- ProxyJump multi-hop chain support
- config CRUD with lossless SSH config preservation
- sshq metadata (tags, env, description) as namespaced comments
- `sshq skill install` for Claude Code and Codex
- `sshq docs --skill` auto-generates scenario-grouped reference docs
- goreleaser + GitHub Actions for Linux/macOS/Windows amd64+arm64 releases
