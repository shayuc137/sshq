# Policy & Audit

Documentation version: `sshq v0.2.0`.

Commands for validating capability policy, managing temporary daemon grants, and querying audit logs.

Auto-generated from `sshq docs --skill`. Do not edit manually.

## sshq policy

```
sshq policy
```

Inspect and manage capability policy

### sshq policy grant

```
sshq policy grant <alias> <pattern> [flags]
```

Temporarily grant a policy whitelist exception in the daemon

**Flags:**

```
      --kind string    grant kind: command, local-path, remote-path, local-forward, or remote-forward (default "command")
      --ttl duration   grant TTL, required and capped at 1h
```

### sshq policy revoke

```
sshq policy revoke [grant-id] [flags]
```

Revoke temporary policy grants

**Flags:**

```
      --alias string   revoke all grants for an alias
```

### sshq policy list

```
sshq policy list [alias]
```

List effective policy and temporary daemon grants

### sshq policy validate

```
sshq policy validate
```

Validate config.toml policy syntax and references

### sshq policy check

```
sshq policy check <alias> [flags]
```

Check whether a command, path, or forward target would be allowed

**Flags:**

```
      --command string          command text to check
      --local-forward string    local forward target (host:port) to check
      --local-path string       local path to check
      --remote-forward string   remote forward target (host:port) to check
      --remote-path string      remote path to check
```

## sshq audit

```
sshq audit [flags]
```

Query structured audit logs

**Flags:**

```
      --alias string       filter audit entries by host alias
      --last int           show the last N audit entries (default 50)
      --operation string   filter audit entries by operation
```

---

## Agent notes

### policy check output

Returns `{decision: {allowed, alias, kind, reason, pattern, input}}`. Exit 0 regardless of allowed/denied — the decision is in the data, not the exit code.

### policy grant behavior

- Requires a controlling TTY (agents cannot self-grant)
- TTL maximum is 1 hour
- Grants live only in daemon memory; daemon restart clears them
- Grants never override `command_blacklist` — blacklist always wins
- Supported kinds: `command`, `local-path`, `remote-path`, `local-forward`, `remote-forward`

### audit output

Returns an array of JSONL entries with: `timestamp`, `alias`, `operation`, `summary`, `result` (success/error/blocked), `duration_ms`, `source` (direct/daemon), `exit_code`.

Blocked entries include `blocked_by` (reason) and `matched_pattern`.
