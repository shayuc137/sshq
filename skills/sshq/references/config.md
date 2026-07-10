# Configuration Management

Documentation version: `sshq v0.2.0`.

Commands for managing SSH host configuration and sshq metadata.

Auto-generated from `sshq docs --skill`. Do not edit manually.

## sshq config

```
sshq config
```

Manage SSH host configuration

### sshq config add

```
sshq config add <alias> [flags]
```

Add a new SSH host

**Flags:**

```
      --desc string         host description
      --env string          environment identifier
      --hostname string     remote hostname or IP (required)
      --identity string     identity file path
      --port string         SSH port
      --proxy-jump string   ProxyJump host
      --tag string          comma-separated tags
      --user string         SSH user
```

### sshq config set

```
sshq config set <alias> <key> <value>
```

Set SSH properties (hostname, user, port, identityfile, proxyjump)
or sshq metadata (tags, env, description) on an existing host.

Examples:
  sshq config set myhost hostname 10.0.0.1
  sshq config set myhost tags prod,web
  sshq config set myhost description "production web server"

### sshq config remove

```
sshq config remove <alias>
```

Remove a host from SSH config

### sshq config list

```
sshq config list
```

List configured hosts (alias for 'sshq ls')

---

## sshq metadata format

sshq stores extended metadata as `# sshq:key=value` comments directly above the `Host` line in `~/.ssh/config`. Standard SSH tools ignore these comments.

**Example host entry:**

```ssh-config
# sshq:description=Production web server
# sshq:tags=prod,web,nginx
# sshq:environment=production
# sshq:created_at=2026-06-25 10:30:00
# sshq:updated_at=2026-06-25 10:30:00
Host prod-web
    HostName 10.0.1.100
    User root
    Port 22
    IdentityFile ~/.ssh/id_ed25519
```

**Metadata keys:**

| Key | Set via | Used by |
|-----|---------|--------|
| `description` | `sshq config set <alias> description "..."` | `sshq ls`, `sshq search` |
| `tags` | `sshq config set <alias> tags prod,web` | `sshq cluster exec --tag` |
| `environment` | `sshq config set <alias> env staging` | `sshq cluster exec --env` |
| `created_at` | Auto-set on `config add` | informational |
| `updated_at` | Auto-set on `config set` | informational |

**SSH properties managed through config set:**

```bash
sshq config set myhost hostname 10.0.0.1
sshq config set myhost user deploy
sshq config set myhost port 2222
sshq config set myhost identityfile ~/.ssh/deploy_key
sshq config set myhost proxyjump bastion
```

**ProxyJump:** Configure in `~/.ssh/config` using standard `ProxyJump` directive. sshq resolves multi-hop chains automatically — just use the target alias.
