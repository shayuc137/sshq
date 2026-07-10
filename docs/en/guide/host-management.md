# Host Management

`sshq` reads and edits OpenSSH-style host entries. The default config path is `~/.ssh/config`; use global `--config <path>` for another file.

## Add Hosts

Use `sshq config add <alias>` to append a host block.

```bash
sshq config add web-1 \
  --hostname 10.0.1.11 \
  --user deploy \
  --port 22 \
  --identity ~/.ssh/deploy_ed25519 \
  --desc "production web server" \
  --tag prod,web \
  --env production \
  --proxy-jump bastion
```

| Option | Meaning |
| --- | --- |
| `--hostname <host>` | Remote host name or IP address. Required. |
| `--port <port>` | SSH port. Default `22` is omitted in new blocks. |
| `--user <user>` | SSH user. |
| `--identity <path>` | Identity file path. |
| `--desc <text>` | Description metadata. |
| `--tag <a,b>` | Comma-separated `tags` metadata. |
| `--env <name>` | `environment` metadata. |
| `--proxy-jump <alias>` | ProxyJump host alias. |

> [!WARNING]
> `sshq` refuses to store passwords in SSH config. For password authentication, use the encrypted credential store (see [Password Credentials](#password-credentials) below).

## Edit Hosts

Use `sshq config set <alias> <key> <value>` to update one field.

```bash
sshq config set web-1 hostname 10.0.1.12
sshq config set web-1 user deploy
sshq config set web-1 port 2222
sshq config set web-1 identityfile ~/.ssh/deploy_ed25519
sshq config set web-1 proxyjump bastion
sshq config set web-1 tags prod,web
sshq config set web-1 env production
sshq config set web-1 description "production web server"
```

`env` and `environment` both write the canonical `environment` metadata key. `password` is rejected:

```text
Error: passwords must not be stored in ssh config
  -> use ssh-agent or identity files
```

## Remove Hosts

```bash
sshq config remove web-1
sshq config rm web-1
```

If the alias does not exist, `sshq` reports the missing host and suggests `sshq ls`.

## List, Search, And Info

```bash
sshq ls
sshq config list
```

`config list` is an alias for `ls`.

Search is case-insensitive, sorted by alias, and checks alias, host name, and description.

```bash
sshq search api
sshq search production
sshq search 10.0.1
```

Inspect one host:

```bash
sshq info api-prod
```

`info` shows alias, host name, user, port, identity file, ProxyJump, metadata, and any cached remote profile.

## Metadata

`sshq` stores non-secret metadata as comments directly above the `Host` line. Standard SSH tools ignore these comments.

```ssh-config
# ===== api-prod =====
# sshq:description=production API node
# sshq:environment=production
# sshq:tags=prod,api
Host api-prod
    HostName api-1.example.internal
    User deploy
    IdentityFile ~/.ssh/prod_ed25519
    ProxyJump bastion
```

| Key | Set with | Used by |
| --- | --- | --- |
| `description` | `sshq config set <alias> description "..."` | `sshq search`, `sshq info` |
| `tags` | `sshq config set <alias> tags prod,web` | `sshq cluster exec --tag` |
| `environment` | `sshq config set <alias> env production` | `sshq cluster exec --env` |

> [!TIP]
> Keep tags short and stable, such as `web`, `db`, `linux`, `prod`, and `staging`.

## ProxyJump

Configure bastion access with standard OpenSSH `ProxyJump`.

```bash
sshq config add bastion --hostname bastion.example.com --user deploy --identity ~/.ssh/deploy_ed25519
sshq config add db-prod --hostname db.internal --user postgres --identity ~/.ssh/prod_ed25519 --proxy-jump bastion
```

Use the target alias directly:

```bash
sshq db-prod "hostname"
sshq cp ./schema.sql db-prod:/tmp/schema.sql
sshq tunnel start db-prod -L 15432:localhost:5432
```

`sshq` resolves multi-hop ProxyJump chains from configured aliases. If `app-prod` jumps through `bastion-2`, and `bastion-2` jumps through `bastion-1`, use `app-prod` as the final target.

> [!WARNING]
> A cyclic ProxyJump chain is cut when a repeated host is detected.

## Password Credentials

Some hosts only support password authentication (legacy switches, certain Windows OpenSSH servers). `sshq` encrypts passwords with [age](https://github.com/FiloSottile/age) using your SSH public key and stores them in the OS config directory (`~/.config/sshq/credentials.age` on Linux, `~/Library/Application Support/sshq/` on macOS, `%AppData%\sshq\` on Windows).

```bash
sshq credential set router-1
sshq credential list
sshq credential delete router-1
```

`credential set` prompts for the password interactively (requires a terminal). Passwords are used as the lowest-priority authentication fallback — agent and key authentication always take precedence.

> [!TIP]
> If you have no SSH key, `sshq` falls back to passphrase-based encryption for the credential file.

For headless environments (daemon, agent pipe), set `SSHQ_CREDENTIAL_PASSPHRASE` in the environment before starting commands. See the [Security guide](security.md) for the full credential and policy configuration.

## Diagnose with doctor

`sshq doctor <alias>` runs seven checks in order — config validity, identity file, ProxyJump resolution, TCP reachability, host key, authentication, and shell detection. It stops at the first failure and returns a `next_action` you can run directly.

```bash
sshq doctor api-prod
```

All checks pass:

```json
{
  "ok": true,
  "data": {
    "alias": "api-prod",
    "resolved": {
      "hostname": "10.0.1.100",
      "port": "22",
      "user": "deploy",
      "proxy_jump": "bastion",
      "identity_file": "~/.ssh/prod_ed25519"
    },
    "checks": {
      "config_valid": true,
      "identity_file_exists": true,
      "proxy_jump_exists": true,
      "tcp_reachable": true,
      "host_key_known": true,
      "auth_ok": true,
      "shell_detected": true
    },
    "profile": {
      "os": "linux",
      "shell": "bash",
      "home_dir": "/home/deploy",
      "detected_at": 1783615160
    }
  },
  "schema_version": 2
}
```

A failure — host key not in `known_hosts`:

```json
{
  "ok": true,
  "data": {
    "alias": "api-prod",
    "failed_check": "host_key_known",
    "hint": "host key is not present in known_hosts",
    "next_action": "sshq trust api-prod"
  },
  "schema_version": 2
}
```

The `next_action` is the exact command to fix the problem. After running it, re-run `doctor` to confirm the remaining checks pass. Checks after the failure point are marked `"skipped"`. An identity file that was not configured shows as `null` rather than `false`.

## Probe Connectivity

Use `probe` for TCP reachability checks. By default, `probe` follows the full SSH configuration path, including any configured `ProxyJump`.

```bash
sshq probe api-prod
sshq probe api-prod --port 2222
sshq probe --all
```

The result includes a `probe_path` field that indicates how the connection was made — `"via-proxy"` when a ProxyJump was used, `"direct"` otherwise:

```json
{
  "ok": true,
  "data": {
    "alias": "api-prod",
    "host": "10.0.1.100",
    "port": "22",
    "proxy_jump": "bastion",
    "probe_path": "via-proxy",
    "reachable": true,
    "latency_ms": 277
  },
  "schema_version": 2
}
```

Use `--direct` to skip the ProxyJump and test raw TCP reachability to the target:

```bash
sshq probe api-prod --direct
```

This is useful for diagnosing whether a host is unreachable because of a proxy problem or because the target port itself is down.

Refresh the cached remote profile after a successful TCP check:

```bash
sshq probe api-prod --refresh-profile
```

Profile detection connects over SSH and records OS, shell, encoding, and home directory details. On Windows hosts, this step also discovers whether `powershell.exe` or `pwsh` is available.

## Trust Host Keys

`sshq` uses strict host key checking. Use `trust` to fetch a host key and add it to `known_hosts`.

```bash
sshq trust api-prod
sshq trust --all
```

If a key changed, `trust` refuses to overwrite it by default:

```text
Error: host key CHANGED for api-prod (api-1.example.internal:22)
  old: ssh-ed25519 SHA256:old...
  new: ssh-ed25519 SHA256:new...
  -> if expected (e.g. OS reinstall), re-run with --replace
```

Replace a changed key only after verification:

```bash
sshq trust api-prod --replace
```

> [!WARNING]
> Treat host key changes as high-signal security events.

## Daemon Lifecycle

The daemon owns the connection pool, profile cache, and background tunnel registry.

```bash
sshq daemon start
sshq daemon status
sshq daemon stop
```

Most commands still work without the daemon; they use direct SSH connections when the daemon is unreachable. When the daemon is running, supported commands reuse pooled SSH connections. Daemon-owned tunnels stop when the daemon stops. The daemon also shuts down after about `30m` of inactivity.

> [!TIP]
> Start the daemon before repeated `exec`, `cp`, `cluster`, or background `tunnel` work. For one-off commands, direct mode is fine.
