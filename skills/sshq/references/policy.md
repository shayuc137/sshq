# Capability Policy

Commands for validating capability policy and managing temporary daemon grants.

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
      --kind string    grant kind: command, local-path, or remote-path (default "command")
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

Check whether a command or path would be allowed

**Flags:**

```
      --command string       command text to check
      --local-path string    local path to check
      --remote-path string   remote path to check
```

