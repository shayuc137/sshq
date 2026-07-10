# Execution & File Transfer

Documentation version: `sshq v0.3.0`.

Commands for running remote commands and transferring files.

Auto-generated from `sshq docs --skill`. Do not edit manually.

## sshq exec

```
sshq exec <alias> <command...> [flags]
```

Execute a command on a remote host

**Examples:**

```bash
sshq exec myhost "uname -a"
sshq exec myhost --script-file ./deploy.sh --shell bash --no-daemon
sshq exec windows-host --script-file ./diagnose.ps1 --shell powershell
```

**Flags:**

```
      --no-daemon            skip daemon, connect directly
      --script-file string   execute a local script file on the remote host
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
      --mkdirs      create missing remote destination parent directories
      --no-daemon   skip daemon, connect directly
  -r, --recursive   copy directories recursively
```

---

## Agent notes

### exec output contract

In pretty mode, process stdout is the remote stdout exactly — sshq never writes its own messages there. In JSON mode, `data.stdout` and `data.stderr` carry the remote streams separately.

| Field | Type | Description |
|-------|------|-------------|
| `exit_code` | int | Remote process exit code (0 = success) |
| `stdout` | string | Remote stdout verbatim |
| `stderr` | string | Remote stderr verbatim |
| `host` | string | Alias used |
| `duration_ms` | int | Wall-clock milliseconds |

`ok:true` means the sshq call succeeded; ALWAYS check top-level `exit_code` for the remote command result. The same value remains in `data.exit_code` for compatibility.

Wrong: treat `{"ok":true,"exit_code":3,...}` as remote command success.

Correct: treat `ok:true` as a completed sshq call, then read top-level `exit_code`; `3` means the remote command failed with exit code 3.

### PowerShell script files

For complex Windows commands, prefer `--script-file <path> --shell powershell`; this avoids local-shell expansion of PowerShell `$variables` and nested quoting. Scripts up to 8 KiB run as `powershell -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand <base64(UTF-16LE(script))>`. Larger scripts automatically use upload-run: sshq uploads a UTF-8-with-BOM temporary `.ps1`, executes it with the same flags plus `-File`, then removes the remote file. The bash, ash, sh, and zsh script paths continue to execute through stdin.

### cp output contract

| Field | Type | Description |
|-------|------|-------------|
| `direction` | string | upload / download / relay |
| `remote` | string | Remote path |
| `size` | int | Total bytes transferred |
| `duration` | string | Human-readable duration |
| `engine` | string | sftp / raw / sftp→sftp |
| `files` | int | File count |

### Exit behavior

- Exit 0: operation succeeded (for exec, the remote result is in top-level `exit_code`)
- Exit 1: connection failure, policy block, or local error
- Exit N (exec only): matches the remote exit code from `sshq exec <alias> "cmd"`

### Security

- exec: checked against `command_whitelist` / `command_blacklist`
- cp: local and remote paths checked against `local_path_whitelist` / `remote_path_whitelist`
- `--script-file`: audit records SHA-256 hash and byte count, not the script content
