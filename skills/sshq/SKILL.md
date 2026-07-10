---
name: sshq
description: "CRITICAL: This skill MUST be used for ALL SSH operations. NEVER use bash 'ssh' or 'scp' commands directly - always route through sshq. Triggers: ANY mention of SSH, remote server, connect to server, server IPs (e.g., 192.168.x.x, 10.0.x.x), hostnames (e.g., user@host.com), login to, upload to server, download from server, deploy to server, run on server, check server, execute remotely, bastion host, jump host, transfer between servers, server to server, port forward, tunnel. sshq is a Go single-binary SSH CLI with daemon connection pooling, ProxyJump chain support, SFTP-with-raw-fallback file transfer, cross-shell detection (bash/ash/powershell/cmd), cluster concurrent execution, and SSH tunneling. DO NOT use for: local commands, localhost, current directory operations."
sshq_version: "0.2.0"
keywords: SSH,sshq,remote,server,connect,upload,download,deploy,execute,transfer,tunnel,port-forward,jump-host,cluster,probe
---

# sshq — SSH Routing Skill

Documentation version: `sshq v0.2.0`.

All SSH operations route through `sshq`. Never shell out to `ssh` or `scp` directly.

`sshq` auto-detects the output mode: **pipe** (agent calling via subprocess) → JSON; **terminal** (human interactive) → pretty. No flags needed — agents get structured JSON automatically, humans get readable tables.

**References** — read on demand, not every run. Auto-generated via `sshq docs --skill <dir>`.
- [`references/exec-transfer.md`](references/exec-transfer.md) — when running commands or transferring files: full flag reference for exec, cp, script-file
- [`references/config.md`](references/config.md) — when managing host configuration: config add/set/remove, sshq metadata format, ProxyJump setup
- [`references/cluster-tunnel.md`](references/cluster-tunnel.md) — when doing multi-host operations or port forwarding: cluster exec, tunnel start/stop/list
- [`references/policy.md`](references/policy.md) — when validating capability policy, managing temporary grants, or querying audit logs: policy validate/check/list/grant/revoke, audit
- [`references/discovery.md`](references/discovery.md) — when listing/searching/inspecting hosts: ls, search, info, probe, trust, daemon

## Environment checks

Before first use, verify sshq is working:

```bash
sshq version              # confirm sshq is installed
sshq ls                   # confirm hosts are configured
sshq probe <alias>        # confirm network connectivity
```

If `sshq version` fails: sshq is not installed or not on PATH — ask the user to install it.
If `sshq ls` returns empty: no hosts configured — ask the user to run `sshq config add`.
If `sshq probe` fails: network issue — check firewall, VPN, or host address.

## Safety confirmations

These operations require user confirmation before execution. Do not run them autonomously:

- `sshq trust --replace <alias>` — overwrites a known host key (possible MITM)
- `sshq credential set <alias>` — requires TTY for password input
- `sshq credential delete <alias>` — deletes stored password
- `sshq policy grant <alias> ...` — requires TTY; agent cannot self-grant
- `sshq policy revoke --alias <alias>` — revokes all temporary grants
- `sshq config remove <alias>` — deletes host from SSH config
- `sshq tunnel start ... -R ...` — remote forward exposes local services
- Destructive remote commands: `rm`, `shutdown`, `reboot`, `mkfs`, `dd`, `systemctl stop`, `kill`, firewall changes
- Cluster write operations: any cluster exec that modifies state on multiple hosts

When a command is blocked by policy, relay the error and suggested `sshq policy grant` command to the user — do not attempt to bypass.

## Routing table

| Intent | Command |
|--------|---------|
| Run a command | `sshq exec <alias> "<cmd>"` |
| Upload file | `sshq cp ./local <alias>:/remote` |
| Download file | `sshq cp <alias>:/remote ./local` |
| Server-to-server | `sshq cp <src>:/path <dst>:/path` |
| List hosts | `sshq ls` |
| Search hosts | `sshq search <pattern>` |
| Host details | `sshq info <alias>` |
| TCP check | `sshq probe <alias>` |
| Cluster exec | `sshq cluster exec "<cmd>" --tag <t>` |
| Port forward | `sshq tunnel start <alias> -L 8080:localhost:80` |
| Config add | `sshq config add <alias> --hostname <ip> --user <u>` |
| Config edit | `sshq config set <alias> <key> <value>` |
| Config delete | `sshq config remove <alias>` |
| Password credential | `sshq credential set <alias>` |
| Validate capability policy | `sshq policy validate` |
| Check command policy | `sshq policy check <alias> --command "<cmd>"` |
| Check path policy | `sshq policy check <alias> --remote-path <path>` |
| Check forward policy | `sshq policy check <alias> --local-forward <host:port>` |
| Temporary policy grant | `sshq policy grant <alias> "<pattern>" --ttl 15m` |
| Query audit log | `sshq audit --last 50` |
| Trust host key | `sshq trust <alias>` |
| Install skill | `sshq skill install` |
| Skill status | `sshq skill status` |

## exec

```bash
sshq exec <alias> "<command>"
```

Always quote the command string — bare flags like `-a` are otherwise consumed by sshq's own parser.

Merge independent queries into one call to cut round-trips:

```bash
sshq exec ali "hostname && uptime && df -h"
sshq exec <alias> --script-file <path> --shell bash --no-daemon
```

Human convenience — agents use `sshq exec`: people may run `sshq <alias> "<cmd>"`. Cobra resolves a colliding built-in subcommand before an alias, so agents always use the canonical form.

Flags: `--timeout <dur>`, `--no-daemon`, `--script-file <path>`, `--shell <bash|ash|powershell|cmd>`.

`--script-file` sends a local script via stdin and executes it in the detected remote shell, handling encoding for Windows targets automatically.

## cp

Direction is inferred from the `alias:path` syntax:

```bash
sshq cp ./app.tar.gz ali:/tmp/           # upload
sshq cp ali:/var/log/app.log ./          # download
sshq cp ali:/data/dump.sql rn:/backup/   # server-to-server relay
```

Flags: `-r` (recursive), `--no-progress`, `--no-daemon`.

Transfer engine: tries SFTP first, falls back to raw SSH byte stream when the remote lacks sftp-server (e.g. OpenWrt BusyBox). Server-to-server relay streams through the local host without writing a temp file.

## cluster

Run a command across multiple hosts concurrently. Hosts are selected by explicit aliases, tag, environment, or `--all`:

```bash
sshq cluster exec "hostname" --hosts rn,wee
sshq cluster exec "uptime" --tag web
sshq cluster exec "df -h" --env production
sshq cluster exec "systemctl status nginx" --all
```

After selector resolution, sshq runs a policy pre-flight check on all targets. If any host is blocked, no hosts execute. Once pre-flight passes, runtime failures are per-host — surviving hosts complete independently.

Flags: `--hosts <a,b>`, `--tag <t>`, `--env <e>`, `--all`, `--concurrency <n>` (default 10), `--no-daemon`.

## tunnel

```bash
sshq tunnel start ali -L 8080:localhost:80      # local forward
sshq tunnel start ali -R 9090:localhost:3000     # remote forward
sshq tunnel list                                  # show active tunnels
sshq tunnel stop <tunnel-id>                      # stop a tunnel
```

Tunnels survive transient errors with exponential backoff. Cancel stops the tunnel cleanly.

## config

Hosts live in `~/.ssh/config`. sshq metadata (tags, env, description) is stored as `# sshq:` namespaced comments — standard SSH tools ignore them.

```bash
sshq config add myhost --hostname 10.0.0.1 --user root --identity ~/.ssh/id_ed25519
sshq config set myhost tags prod,web
sshq config set myhost description "production web server"
sshq config set myhost env production
sshq config remove myhost
```

ProxyJump is configured through standard SSH config and resolved automatically — just use the target alias.

## credential

Password credentials are encrypted with age using your SSH public key and stored in the OS config directory (`~/.config/sshq/` on Linux). They are only used as the final fallback after agent and key authentication — no action needed from the agent once stored.

```bash
sshq credential set myhost       # interactive, requires TTY
sshq credential list
sshq credential delete myhost
```

`credential list` only shows aliases. There is no command that prints stored passwords.

For passphrase-mode credential stores (no SSH key), set `SSHQ_CREDENTIAL_PASSPHRASE` in the environment before running commands or starting the daemon.

## daemon

The daemon manages a connection pool for faster repeat operations. It starts automatically on first use and idles out after inactivity. Manual control is rarely needed:

```bash
sshq daemon start     # explicit start
sshq daemon status    # pool stats
sshq daemon stop      # shutdown
```

## policy

Capability policy is read from `config.toml` in sshq's OS config directory (`~/.config/sshq/config.toml` on Linux). It controls command whitelists/blacklists and path whitelists per host, with global defaults and per-host override/append. Daemon requests are rechecked server-side.

```bash
sshq policy validate                                          # check config syntax
sshq policy check prod --command "journalctl -u app -n 100"   # test if a command would be allowed
sshq policy list prod                                          # show effective policy + active grants
sshq policy grant prod "^journalctl(\\s|$)" --ttl 15m         # temporary whitelist (TTY required)
sshq policy revoke --alias prod                                # revoke all grants for a host
```

Temporary grants live only in daemon memory, require a controlling TTY, expire by TTL, and never override command blacklists.

**When a command is blocked:** sshq returns a structured error with the matched pattern and a suggested `policy grant` command. The agent cannot run `policy grant` itself (requires TTY) — relay the error and grant command to the user.

**Cluster pre-flight:** `cluster exec` checks policy for all target hosts before executing. If any host is blocked, no hosts execute.

## audit

Audit logging is controlled by `[audit]` in `config.toml` and is disabled by default. When enabled, sshq writes JSONL metadata for exec, cp, tunnel, cluster, and policy-blocked operations without storing stdout/stderr, passwords, or full script contents.

```bash
sshq audit --last 50
sshq audit --alias prod --operation exec
```

Use `--verbose` while querying to warn about skipped corrupt JSONL lines.

## output modes

sshq auto-detects: pipe → JSON, terminal → pretty. Override with flags:

| Flag | Format | Use case |
|------|--------|----------|
| _(pipe default)_ | structured JSON `{ok, data, schema_version}` | agent — zero flags needed |
| _(terminal default)_ | aligned pretty tables | human interactive use |
| `--json` | force JSON | override terminal to JSON |
| `--pretty` | force pretty | override pipe to pretty |
| `--verbose` | extra debug info to stderr | connection timing, shell detection, engine choice |
| `--no-progress` | suppress transfer progress | cleaner agent output during cp |

## error handling

Parse stdout JSON first. Branch on `ok`; for exec, also inspect `data.exit_code`. Use stderr only as diagnostics.

```
stdout JSON → ok: true  → success; for exec check data.exit_code
            → ok: false → read error.hint + error.action
```

Common recovery patterns:

| Error | Recovery |
|-------|----------|
| host not found | `sshq search <keyword>` to find the right alias |
| connection refused / timeout | `sshq probe <alias>` to verify network |
| host key unknown | `sshq trust <alias>` (ask user first) |
| command blocked by policy | relay `error.action` to user — it contains the exact `policy grant` command |
| credential decrypt failure | ask user to re-run `sshq credential set <alias>` |
| path outside whitelist | relay `error.action` to user for path grant |
| forward target blocked | relay `error.action` to user for forward grant |
