# Cluster & Tunnel

Documentation version: `sshq v0.2.0`.

Commands for concurrent multi-host operations and port forwarding.

Auto-generated from `sshq docs --skill`. Do not edit manually.

## sshq cluster

```
sshq cluster
```

Concurrent operations across multiple hosts

### sshq cluster exec

```
sshq cluster exec <command> [flags]
```

Execute a command on multiple hosts concurrently

**Flags:**

```
      --all               target all configured hosts
      --concurrency int   max concurrent connections (default 10)
      --env string        filter hosts by environment
      --hosts string      comma-separated host aliases
      --no-daemon         skip daemon, connect directly
      --tag string        filter hosts by tag
```

## sshq tunnel

```
sshq tunnel
```

Manage SSH port forwarding

### sshq tunnel start

```
sshq tunnel start <alias> [flags]
```

Start an SSH tunnel for port forwarding.

Examples:
  sshq tunnel start ali -L 8080:localhost:80     local forward
  sshq tunnel start ali -R 9090:localhost:3000    remote forward

**Flags:**

```
  -L, --L string   local forward: <local_port>:<remote_host>:<remote_port>
  -R, --R string   remote forward: <remote_port>:<local_host>:<local_port>
```

### sshq tunnel stop

```
sshq tunnel stop <id>
```

Stop a tunnel

### sshq tunnel list

```
sshq tunnel list
```

List active tunnels

---

## Agent notes

### cluster output contract

JSON mode returns an envelope with top-level `exit_code` and `data: {results: [{alias, stdout, stderr, exit_code, error}], summary: {total, success, failed}}`. The aggregate exit code is the first non-zero host exit code in alias order, or 1 for a host transport error when no remote command returned non-zero.

### cluster policy pre-flight

After selector resolution, sshq checks policy for all targets before execution. If any host is blocked, no hosts execute. Pre-flight block is exit 1 with a policy error, not a partial-failure result.

### tunnel output contract

tunnel start returns `{id, direction, local_addr, remote_addr}`. tunnel list returns an array of `{id, direction, alias, local_addr, remote_addr, active_connections}`.

### tunnel forward whitelist

When capability policy is enabled:
- `-L` checks `local_forward_whitelist` against the remote target (`remote_host:remote_port`)
- `-R` checks `remote_forward_whitelist` against the local target (`local_host:local_port`)

Matching supports exact (`host:port`), port wildcard (`host:*`), port range (`host:8000-9000`), and host wildcard (`*:port`).

### Daemon vs foreground

With daemon running, tunnels are background and managed via `tunnel list` / `tunnel stop`. Without daemon, tunnel runs in foreground until Ctrl+C.
