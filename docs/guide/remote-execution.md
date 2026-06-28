# Remote Execution

Use `sshq` to run commands on configured SSH hosts. This guide covers the common execution tasks and the flags that change runtime behavior.

## Basic Exec

The canonical form is:

```bash
sshq exec web-1 "hostname"
```

The shortcut form runs the same `exec` implementation:

```bash
sshq web-1 "hostname"
```

Use the shortcut for simple commands. Use `sshq exec` when you need command-specific flags such as `--script-file`, `--shell`, or `--no-daemon`. In a terminal, remote stdout is written to local stdout and remote stderr is written to local stderr. When an agent calls `sshq` through a pipe, stdout contains a JSON envelope.

## Quote Commands

Always quote the remote command:

```bash
sshq web-1 "uname -a"
sshq web-1 "systemctl status nginx"
```

Quoting keeps remote flags, pipes, redirects, variables, and chaining operators inside the remote command string. This avoids local shell parsing and avoids Cobra treating remote flags as `sshq` flags.

Use single quotes when the remote command contains `$` that must expand on the remote host:

```bash
sshq web-1 'echo "$SHELL"'
```

## Chain Multiple Commands

Run several commands in one SSH round trip by chaining them inside one quoted command string:

```bash
sshq web-1 "cd /srv/app && git pull --ff-only && systemctl restart app"
sshq web-1 "date; uptime; df -h /"
```

Use `&&` for fail-fast chains. Use `;` when later commands should still run after an earlier command exits non-zero. The exit code is the remote shell's exit code for the full command string.

> [!TIP]
> Put complex multi-line logic into `--script-file` once quoting starts to obscure the command.

## Override the Shell

`sshq` detects the remote shell profile and uses it by default. Override it when you need a specific shell syntax:

```bash
sshq exec --shell bash web-1 "set -euo pipefail; echo ok"
sshq exec --shell sh web-1 "echo ok"
```

On Windows, use `--shell powershell` for PowerShell syntax:

```bash
sshq exec --shell powershell win-1 "Get-Service | Where-Object Status -eq 'Running' | Select-Object -First 5"
```

Use `--shell cmd` for one-line `cmd.exe` commands:

```bash
sshq exec --shell cmd win-1 "dir C:\Windows"
```

> [!WARNING]
> `cmd` cannot run `--script-file` through stdin injection. Use `--shell powershell` for Windows scripts.

## Run a Script File

Use `--script-file` when a command is too long or too hard to quote:

```bash
sshq exec --script-file ./scripts/diagnostics.sh web-1
sshq exec --shell powershell --script-file ./scripts/diagnostics.ps1 win-1
```

`sshq` reads the local file and sends its bytes to the remote interpreter through stdin. The script file does not need to exist on the remote host, and it does not need executable permissions.

For POSIX shells, `sshq` starts an interpreter such as `sh -s`, `bash -s`, `ash -s`, or `zsh -s`. For PowerShell, it starts:

```text
powershell -NoProfile -NonInteractive -Command -
```

Run a local health-check script:

```bash
sshq exec --script-file ./scripts/health-check.sh web-1
```

## Control Timeouts

`--timeout` is a global flag. The default timeout is `30s`. For remote execution, it covers connection setup, shell detection, and command or script execution.

```bash
sshq --timeout 10s web-1 "uptime"
sshq --timeout 2m exec --script-file ./scripts/slow-maintenance.sh web-1
sshq --timeout 15m web-1 "apt-get update && apt-get install -y jq"
```

If the timeout expires during execution, `sshq` cancels the SSH session and returns an error.

## Windows Encoding

Windows hosts can emit text in a local code page such as `GBK`. During profile detection, `sshq` checks the Windows code page with `chcp`.

For known non-UTF-8 encodings, command stdout and stderr are transcoded to UTF-8 before rendering:

```bash
sshq exec --shell powershell win-1 "Get-ChildItem C:\"
```

Known mappings include `936` to `gbk`, `950` to `big5`, `932` to `shift-jis`, and `949` to `euc-kr`. Code page `65001` is already UTF-8 and is passed through.

## Direct vs Daemon

If the daemon is already running, `sshq` can dispatch execution through it and reuse pooled SSH connections. If the daemon is unavailable or dispatch fails, `sshq` falls back to a direct connection.

Start and inspect the daemon manually:

```bash
sshq daemon start
sshq daemon status
```

Use `--no-daemon` when debugging connection setup, shell detection, or daemon-specific behavior:

```bash
sshq exec --no-daemon web-1 "hostname"
sshq exec --no-daemon --script-file ./scripts/diagnostics.sh web-1
```

For normal usage, omit `--no-daemon`.

## Verbose Mode

Use `--verbose` or `-v` to print diagnostics to stderr:

```bash
sshq -v web-1 "hostname"
```

Verbose output can include connection timing, daemon reuse, detected OS and shell, selected shell, and execution details:

```text
connection: alias=web-1 duration=18ms daemon reused=true
profile: os=linux shell=bash
shell selected: bash
```

Verbose output stays on stderr. Remote stdout remains clean in terminal mode, and JSON output stays on stdout in agent mode.

## Common Patterns

Check disk space on one host:

```bash
sshq web-1 "df -h /"
```

Check disk space across hosts:

```bash
sshq cluster exec "df -h /" --hosts web-1,web-2,db-1
```

Deploy by running a local script:

```bash
sshq exec --script-file ./scripts/deploy.sh web-1
```

Run basic diagnostics:

```bash
sshq web-1 "hostname && uptime && df -h / && free -m"
```

Run Windows diagnostics with PowerShell:

```bash
sshq exec --shell powershell win-1 "Get-ComputerInfo | Select-Object CsName,WindowsVersion,OsUptime"
```
