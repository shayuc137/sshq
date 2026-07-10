# Discovery & Daemon

Commands for listing, searching, inspecting hosts, credentials, and managing the daemon.

Auto-generated from `sshq docs --skill`. Do not edit manually.

## sshq ls

```
sshq ls
```

List configured SSH hosts

## sshq search

```
sshq search <pattern>
```

Search SSH hosts by pattern

## sshq info

```
sshq info <alias>
```

Show detailed host information

## sshq probe

```
sshq probe <alias> [flags]
```

Check TCP connectivity to a host

**Flags:**

```
      --all               probe all configured hosts
      --port string       override port to probe
      --refresh-profile   detect and cache remote OS/shell profile
```

## sshq trust

```
sshq trust [alias] [flags]
```

Fetch the SSH host key from a remote server and add it to known_hosts.
If the key has changed (mismatch), use --replace to update it.

**Flags:**

```
      --all       trust all configured hosts
      --replace   replace mismatched host keys
```

## sshq credential

```
sshq credential
```

Manage encrypted password credentials

### sshq credential set

```
sshq credential set <alias>
```

Store an encrypted password credential

### sshq credential delete

```
sshq credential delete <alias>
```

Delete an encrypted password credential

### sshq credential list

```
sshq credential list
```

List aliases with stored password credentials

## sshq daemon

```
sshq daemon
```

Manage the connection pool daemon

### sshq daemon start

```
sshq daemon start
```

Start the connection pool daemon

### sshq daemon stop

```
sshq daemon stop
```

Stop the daemon

### sshq daemon status

```
sshq daemon status
```

Show daemon status

## sshq version

```
sshq version
```

Print version information

## sshq skill

```
sshq skill
```

Install and inspect sshq AI skill files

### sshq skill install

```
sshq skill install [flags]
```

Install the embedded sshq skill

**Flags:**

```
      --codex     install for Codex instead of Claude Code
      --dry-run   print file paths without writing
      --project   install at project level instead of user level
```

### sshq skill export

```
sshq skill export [flags]
```

Export the embedded sshq skill files

**Flags:**

```
      --dir string   output directory (default "./sshq-skill/")
```

### sshq skill status

```
sshq skill status
```

Show installed sshq skill versions

---

## Agent notes

### Security-sensitive commands

- `trust --replace`: overwrites a known host key — ask user first (possible MITM)
- `credential set`: requires TTY for password input — relay to user
- `credential delete`: permanently deletes a stored password — ask user first
- `credential list`: only shows aliases, never prints passwords

### daemon status output

Returns `{running, uptime_seconds, connections: [{alias, host, idle}]}`.

### skill commands

- `skill install`: installs sshq skill to Claude Code (`--target codex` for Codex, `--scope project` for project-level)
- `skill status`: shows install location and version
- `skill export`: prints skill files to stdout (for manual installation)
