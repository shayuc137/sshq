# Getting Started

This tutorial takes you from a fresh install to a successful remote command. The examples use `myhost` as the sshq alias, `10.0.0.1` as the host IP, `root` as the user, and `~/.ssh/id_ed25519` as the private key.

## Prerequisites

You need:

- Go 1.23 or newer, if you install with `go install`
- An SSH key pair on your local machine
- At least one reachable SSH host

Check Go, your key files, and create a key if needed:

```bash
go version
ls ~/.ssh/id_ed25519 ~/.ssh/id_ed25519.pub
ssh-keygen -t ed25519 -f ~/.ssh/id_ed25519
```

Make sure the remote host accepts your public key before adding it to sshq.

> [!NOTE]
> sshq uses standard SSH configuration, identity files, ssh-agent, and known_hosts behavior.

## Install sshq

Install with Go and make sure the Go binary directory is on `PATH`:

```bash
go install github.com/shayuc137/sshq/cmd/sshq@latest
export PATH="$PATH:$(go env GOPATH)/bin"
```

You can also download a prebuilt binary from GitHub Releases:

```bash
# Download the archive for your OS and CPU from:
# https://github.com/shayuc137/sshq/releases
```

Place the `sshq` binary somewhere on `PATH`, such as `/usr/local/bin` on Linux or macOS.

Verify the install:

```bash
sshq version
```

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
{"ok":true,"data":{"exit_code":0,"stdout":"myhost\n","stderr":"","host":"myhost","duration_ms":42},"schema_version":1}
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

- [Agent Integration](agent-integration.md)
- [Command Reference](../commands/sshq.md)
- [sshq exec](../commands/sshq_exec.md)
- [sshq cp](../commands/sshq_cp.md)
- [sshq config add](../commands/sshq_config_add.md)
- [sshq cluster exec](../commands/sshq_cluster_exec.md)
- [sshq tunnel](../commands/sshq_tunnel.md)

After your first command works, try file transfer with `sshq cp`, multi-host execution with `sshq cluster exec`, and port forwarding with `sshq tunnel start`.
