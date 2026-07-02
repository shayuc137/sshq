# Security

`sshq` provides three security layers: encrypted credential storage, capability policy, and audit logging. All configuration lives in `config.toml` under the OS config directory:

- Linux: `~/.config/sshq/`
- macOS: `~/Library/Application Support/sshq/`
- Windows: `%APPDATA%\sshq\`

## Password Credentials

Some hosts only support password authentication (legacy switches, certain Windows OpenSSH servers). `sshq` encrypts passwords with [age](https://github.com/FiloSottile/age) using your SSH public key and stores them in `credentials.age` alongside `config.toml`.

```bash
sshq credential set router-1       # interactive prompt, requires terminal
sshq credential list                # show aliases only, never passwords
sshq credential delete router-1
```

Passwords are the lowest-priority authentication fallback — SSH agent and key authentication always take precedence. If you have no SSH key, `sshq` falls back to passphrase-based encryption for the credential file.

For headless environments (daemon, agent pipe), set the `SSHQ_CREDENTIAL_PASSPHRASE` environment variable before starting commands or the daemon.

> [!WARNING]
> There is no `credential get` command by design — reducing the attack surface for stored passwords.

## Capability Policy

Capability policy controls what commands an agent can execute and what paths it can access. Configure it in `config.toml`:

```toml
[policy.default]
command_whitelist = ["^ls(\\s|$)", "^cat(\\s|$)", "^grep(\\s|$)", "^tail(\\s|$)"]
command_blacklist = ["(?i)\\brm\\s+-rf\\b", "(?i)\\bmkfs\\b", "(?i)\\bshutdown\\b"]
local_path_whitelist = ["."]
remote_path_whitelist = []

[policy.hosts.prod-db]
mode = "override"
command_whitelist = ["^SELECT\\s", "^SHOW\\s"]
command_blacklist = ["^DROP\\s", "^DELETE\\s", "^TRUNCATE\\s"]
remote_path_whitelist = ["/var/log"]
```

### Global Defaults and Per-Host Override

- `[policy.default]` applies to all hosts
- `[policy.hosts.<alias>]` overrides or appends to defaults
- `mode = "override"` replaces all default arrays; `mode = "append"` (default) adds to them
- `enabled = false` on a per-host section skips policy checks entirely for that host
- No `config.toml` = no policy = no restrictions (backward compatible)

### Whitelist and Blacklist

- **Whitelist** (non-empty): command must match at least one pattern
- **Blacklist**: any match rejects the command
- Order: whitelist check first, then blacklist. Blacklist always wins

Use anchored patterns for safety:

```toml
command_whitelist = ["^journalctl(\\s|$)", "^systemctl\\s+status\\s"]
command_blacklist = ["(?i)(^|[;&|])\\s*(rm|dd|mkfs|shutdown)\\b"]
```

### Path Whitelists

- `local_path_whitelist`: restricts the local side of `cp` operations
- `remote_path_whitelist`: restricts the remote side of `cp` operations
- Empty whitelist = no restriction for that dimension
- Paths are resolved to absolute form with symlink resolution (local) and prefix-boundary matching

### Forward Whitelists

Tunnel forwarding is subject to the same policy framework as commands and paths:

```toml
[policy.default]
local_forward_whitelist = ["localhost:8000-9000", "db.internal:5432"]
remote_forward_whitelist = []
```

- `local_forward_whitelist` checks the **remote target** of `-L` tunnels (`remote_host:remote_port`)
- `remote_forward_whitelist` checks the **local target** of `-R` tunnels (`local_host:local_port`)
- Empty whitelist = no restriction

Matching supports exact (`localhost:8080`), port wildcard (`localhost:*`), port range (`localhost:8000-9000`), and host wildcard (`*:22`).

Test before opening a tunnel:

```bash
sshq policy check bastion --local-forward db.internal:5432
```

Grant temporary forward access:

```bash
sshq policy grant bastion "db.internal:5432" --kind local-forward --ttl 15m
```

### Temporary Grants

When a command is blocked, the error message includes a suggested grant command:

```bash
sshq policy grant prod "^docker restart" --ttl 1h
```

Grants require a terminal (agents cannot self-grant), live only in daemon memory, expire by TTL (max 1 hour), and never override blacklists. Revoke with:

```bash
sshq policy revoke --alias prod     # revoke all grants for a host
sshq policy revoke <grant-id>       # revoke a specific grant
```

### Validation and Testing

```bash
sshq policy validate                                           # check config syntax and regex validity
sshq policy check prod --command "journalctl -u app -n 100"    # dry-run: would this command be allowed?
sshq policy list prod                                           # show effective policy and active grants
```

### Cluster Pre-Flight

`sshq cluster exec` checks policy for all target hosts before executing. If any host is blocked, no hosts execute — preventing partial execution.

> [!TIP]
> For production hosts, use `mode = "override"` with narrow whitelists rather than appending to a broad global default.

## Audit Logging

Audit logging records metadata about every operation (exec, cp, tunnel, cluster) without storing command output, passwords, or full script contents. Controlled via `config.toml`:

```toml
[audit]
enabled = true
path = "~/.config/sshq/audit.jsonl"
max_size = "10MB"
```

### What Gets Recorded

Each operation produces a JSONL entry with timestamp, alias, operation type, command summary (truncated to 200 characters), result (success/error/blocked), duration, and exit code.

Script-file operations record a SHA-256 hash and byte count instead of the full script content. Policy-blocked operations are recorded with `result = "blocked"` and the matched pattern.

### Querying

```bash
sshq audit                               # show recent entries
sshq audit --last 50                      # last 50 entries
sshq audit --alias prod                   # filter by host
sshq audit --operation exec               # filter by operation type
sshq audit --alias prod --operation cp    # combine filters
```

### Log Rotation

When the log file reaches `max_size`, it is rotated to `audit-YYYYMMDD-HHMMSS.log` and a new `audit.log` is created. Query commands scan both current and rotated files.

### Fail-Closed

When audit is enabled but the log file cannot be created or written, operations are blocked rather than proceeding without audit. This prevents "audit says it's on but nothing is recorded."

> [!WARNING]
> Audit logging is append-only and file permissions are set to 0600, but this does not prevent the same OS user or root from modifying the log. For tamper-evidence, consider external log sinks in the future.
