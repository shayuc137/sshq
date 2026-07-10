# Getting Started

This tutorial takes you from a fresh install to a successful remote command. The examples use `myhost` as the sshq alias, `10.0.0.1` as the host IP, `root` as the user, and `~/.ssh/id_ed25519` as the private key.

## Prerequisites

You need:

- An SSH key pair on your local machine
- At least one reachable SSH host

Check your key files and create one if needed:

```bash
ls ~/.ssh/id_ed25519 ~/.ssh/id_ed25519.pub
ssh-keygen -t ed25519 -f ~/.ssh/id_ed25519
```

Make sure the remote host accepts your public key before adding it to sshq.

> [!NOTE]
> sshq uses standard SSH configuration, identity files, ssh-agent, and known_hosts behavior.

## Install sshq

Pick your platform and run the commands below. No Go or other toolchain required.

**Linux (amd64):**

```bash
curl -L https://github.com/shayuc137/sshq/releases/latest/download/sshq_linux_amd64.tar.gz | tar xz
sudo mv sshq /usr/local/bin/
sshq version
```

**Linux (arm64, e.g. Raspberry Pi):**

```bash
curl -L https://github.com/shayuc137/sshq/releases/latest/download/sshq_linux_arm64.tar.gz | tar xz
sudo mv sshq /usr/local/bin/
sshq version
```

**macOS (Apple Silicon):**

```bash
curl -L https://github.com/shayuc137/sshq/releases/latest/download/sshq_darwin_arm64.tar.gz | tar xz
sudo mv sshq /usr/local/bin/
sshq version
```

**macOS (Intel):**

```bash
curl -L https://github.com/shayuc137/sshq/releases/latest/download/sshq_darwin_amd64.tar.gz | tar xz
sudo mv sshq /usr/local/bin/
sshq version
```

**Windows:**

1. Download [`sshq_windows_amd64.zip`](https://github.com/shayuc137/sshq/releases/latest/download/sshq_windows_amd64.zip)
2. Extract the zip — you get `sshq.exe`
3. Move `sshq.exe` to a folder that is already on your PATH, for example `C:\Windows\` or `C:\Users\YourName\bin\`
4. Open a new terminal and run: `sshq version`

> If you are unsure which folders are on PATH, run `echo %PATH%` in cmd or `$env:PATH -split ';'` in PowerShell to see the list. Pick any folder from that list and put `sshq.exe` there.

**Alternative: install from source (requires Go 1.23+):**

```bash
go install github.com/shayuc137/sshq/cmd/sshq@latest
```

> `go install` puts the binary in `$(go env GOPATH)/bin`. If `sshq version` says "command not found", add that directory to PATH: `export PATH="$PATH:$(go env GOPATH)/bin"` (add this line to `~/.bashrc` or `~/.zshrc` to make it permanent).

In a terminal this prints readable text. In an agent subprocess or script it prints a JSON envelope.

## Add Your First Host

Add a host named `myhost`:

```bash
sshq config add myhost \
  --hostname 10.0.0.1 \
  --user root \
  --identity ~/.ssh/id_ed25519
```

Use `--port` when the host uses a non-default SSH port:

```bash
sshq config add myhost \
  --hostname 10.0.0.1 \
  --user root \
  --identity ~/.ssh/id_ed25519 \
  --port 2222
```

Check the saved host:

```bash
sshq ls
sshq info myhost
```

> [!TIP]
> Pick short, stable aliases such as `myhost`, `prod-web-1`, or `lab-router`.

For password-only hosts (legacy switches, certain Windows SSH servers), store the password in the encrypted credential store instead of `~/.ssh/config`:

```bash
sshq credential set myhost
```

See the [Security guide](security.md) for details on credential encryption.

## Test Connectivity

Check whether the configured SSH port is reachable:

```bash
sshq probe myhost
```

If the probe fails, check the IP or DNS name, port, firewall rules, VPN, and network path.

## Trust the Host Key

Fetch the host key and add it to `known_hosts`:

```bash
sshq trust myhost
```

After this succeeds, sshq can connect without an unknown-host-key prompt.

If a host key changes, sshq reports a mismatch and suggests the replacement command:

```bash
sshq trust myhost --replace
```

> [!WARNING]
> Use `--replace` only after you have confirmed the host key change is expected.

## Run Your First Command

Run `hostname` on the remote host:

```bash
sshq myhost "hostname"
```

> [!TIP]
> Always quote remote commands. This keeps flags such as `-a` inside the remote command instead of letting your local shell or sshq parse them.

Try a command with arguments:

```bash
sshq myhost "uname -a"
```

Force structured output for an agent-style result:

```bash
sshq --json myhost "hostname"
```

Example JSON output:

```json
{"ok":true,"exit_code":0,"data":{"exit_code":0,"stdout":"myhost\n","stderr":"","host":"myhost","duration_ms":42},"schema_version":2}
```

## Understand Output Modes

sshq chooses output mode from stdout:

| Caller | stdout type | Default output |
|--------|-------------|----------------|
| Human in a terminal | TTY | Pretty text |
| Agent or script | Pipe | JSON envelope |

Pretty mode is meant for humans:

```bash
sshq myhost "df -h"
```

JSON mode is meant for tools:

```bash
result=$(sshq myhost "df -h")
```

For `exec`, JSON mode returns `exit_code`, `stdout`, `stderr`, `host`, and `duration_ms` under `data`.

sshq informational messages, progress, and verbose diagnostics go to stderr. In pretty mode, process stdout for `exec` is the remote stdout exactly. In JSON mode, the exact remote stdout is in `data.stdout`.

Force an output mode when needed:

```bash
sshq --json myhost "hostname"
sshq --pretty myhost "hostname"
SSHQ_OUTPUT=json sshq myhost "hostname"
```

## Next Steps

After your first command works:

- [Remote Execution](remote-execution.md) — script files, shell override, timeouts
- [File Transfer](file-transfer.md) — upload, download, relay
- [Cluster Operations](cluster-operations.md) — run commands across multiple hosts
- [Tunnels](tunnels.md) — port forwarding
- [Security](security.md) — credential encryption, capability policy, audit logging
- [Agent Integration](agent-integration.md) — JSON contracts, stdout purity, skill install
