# Cluster Operations

`sshq cluster exec` runs one command across many configured SSH hosts. Use it for health checks, rolling operations, and metrics collection.

## Basic Syntax

```bash
sshq cluster exec "<command>" --all
sshq cluster exec "<command>" --tag web
sshq cluster exec "<command>" --env production
sshq cluster exec "<command>" --hosts rn,wee,ali
```

Always quote the remote command. Each run needs a host selector.

| Selector | Target set |
| --- | --- |
| `--all` | Every concrete host alias in the active SSH config. |
| `--tag web` | Hosts whose `tags` metadata contains `web`. |
| `--env production` | Hosts whose `environment` metadata is `production`. |
| `--hosts rn,wee,ali` | Exactly the listed aliases, comma-separated. |

`--tag` and `--env` can be combined. `--hosts` is exclusive and cannot be used with `--tag`, `--env`, or `--all`.

> [!TIP]
> Run `sshq ls` or `sshq search <pattern>` first when the target set is unclear.

## Run On All Hosts

```bash
sshq cluster exec "hostname" --all
sshq cluster exec "uptime" --all
sshq cluster exec "df -h /" --all
```

The host list comes from `~/.ssh/config` by default. Use global `--config <path>` for another file. Wildcard blocks such as `Host *` are not cluster targets.

## Filter By Tag

Tags are stored as comma-separated host metadata. Add or edit them with `config`.

```bash
sshq config add web-1 --hostname 10.0.1.11 --user deploy --tag prod,web
sshq config set web-2 tags prod,web
sshq config set db-1 tags prod,db
```

Run on tagged hosts:

```bash
sshq cluster exec "systemctl is-active nginx" --tag web
```

Tag matching is exact after comma splitting. `prod,web` matches `web`; `websocket` does not.

## Filter By Environment

Environment is stored as the `environment` metadata key.

```bash
sshq config add web-1 --hostname 10.0.1.11 --user deploy --env production
sshq config set web-2 env production
sshq config set web-canary env staging
```

Run on one environment, or combine environment with tag:

```bash
sshq cluster exec "uname -a" --env production
sshq cluster exec "systemctl status nginx --no-pager" --tag web --env production
```

## Use Explicit Hosts

Use `--hosts` when the target list should be fixed and independent of metadata.

```bash
sshq cluster exec "hostname" --hosts rn,wee,ali
sshq cluster exec "uptime" --hosts web-1,web-2,web-3
sshq cluster exec "hostname" --hosts "web-1, web-2, web-1"
```

Spaces around aliases are ignored, and duplicate aliases are ignored. Missing aliases fail before any remote command starts:

```text
Error: hosts not found: web-9
  -> run 'sshq ls' to see available hosts
```

## Control Concurrency

Use `--concurrency N` to limit simultaneous hosts.

```bash
sshq cluster exec "uptime" --tag web --concurrency 5
sshq cluster exec "df -h /" --all --concurrency 20
```

The default is `10`. Values less than or equal to zero fall back to the default. Use low concurrency for state-changing commands and higher concurrency for read-only checks.

> [!TIP]
> For rolling changes, start with `--concurrency 1`.

## Result Format

Text output prefixes each remote stdout line with `[alias]` and ends with a summary line.

```bash
sshq cluster exec "hostname" --hosts web-1,web-2
```

```text
[web-1] web-1
[web-2] web-2
total=2 success=2 failed=0
```

Non-zero remote exits and connection errors are shown per host:

```text
[web-1] active
[web-2] inactive
[web-2] exit=3
total=2 success=1 failed=1
```

```text
[web-1] ok
[web-2] error: dial tcp 10.0.1.12:22: i/o timeout
total=2 success=1 failed=1
```

Use `--json` for machine-readable results. The JSON data contains `results` and `summary`; each result includes `alias`, `stdout`, `stderr`, `exit_code`, and optional `error`.

## Error Handling

Partial failures do not stop other hosts. Every selected host gets an individual result. The local command exits non-zero if any host has a connection error, execution error, or non-zero remote exit code.

Selector errors happen before execution:

```text
Error: specify --hosts, --tag, --env, or --all
  -> usage: sshq cluster exec --all "command"
```

```text
Error: --hosts cannot be combined with --tag, --env, or --all
  -> use exactly one host selector
```

## Common Patterns

Health checks:

```bash
sshq cluster exec "systemctl is-active nginx" --tag web --env production
sshq cluster exec "curl -fsS http://127.0.0.1/health" --tag web
```

Rolling restart:

```bash
sshq cluster exec "sudo systemctl restart nginx" --tag web --env production --concurrency 1
```

Gather metrics:

```bash
sshq cluster exec "df -h /" --all
sshq cluster exec "cat /proc/loadavg" --tag linux
sshq cluster exec "free -m" --tag linux --concurrency 20
```

Target a small batch:

```bash
sshq cluster exec "date -u" --hosts web-1,web-2,web-canary
```
