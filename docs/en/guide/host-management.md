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

Some hosts only support password authentication (legacy switches, certain Windows OpenSSH servers). `sshq` encrypts passwords with [age](https://github.com/FiloSottile/age) using your SSH public key and stores them in `~/.config/sshq/credentials.age`.

```bash
sshq credential set router-1
sshq credential list
sshq credential delete router-1
```

`credential set` prompts for the password interactively (requires a terminal). Passwords are used as the lowest-priority authentication fallback — agent and key authentication always take precedence.

> [!TIP]
> If you have no SSH key, `sshq` falls back to passphrase-based encryption for the credential file.

## Probe Connectivity

Use `probe` for TCP reachability checks.

```bash
sshq probe api-prod
sshq probe api-prod --port 2222
sshq probe --all
```

Refresh the cached remote profile after a successful TCP check:

```bash
sshq probe api-prod --refresh-profile
```

Profile detection connects over SSH and records OS, shell, encoding, and home directory details when available.

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
