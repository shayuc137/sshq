# Discovery & Daemon

Commands for listing, searching, inspecting hosts, and managing the daemon.

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

