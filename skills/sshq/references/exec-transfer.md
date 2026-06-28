# Execution & File Transfer

Commands for running remote commands and transferring files.

Auto-generated from `sshq docs --skill`. Do not edit manually.

## sshq exec

```
sshq exec <alias> <command...> [flags]
```

Execute a command on a remote host

**Flags:**

```
      --no-daemon            skip daemon, connect directly
      --script-file string   execute a local script file on the remote host via stdin
      --shell string         override detected remote shell type (bash/ash/zsh/sh/powershell)
```

## sshq cp

```
sshq cp <src> <dst> [flags]
```

Copy files using alias:path syntax to determine direction:
  sshq cp local.txt ali:/tmp/          upload
  sshq cp ali:/var/log/app.log ./      download
  sshq cp ali:/data/f.tar rn:/backup/  server-to-server relay

**Flags:**

```
      --no-daemon     skip daemon, connect directly
      --no-progress   disable progress output
  -r, --recursive     copy directories recursively
```

