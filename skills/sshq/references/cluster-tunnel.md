# Cluster & Tunnel

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

