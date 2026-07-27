# Agent Integration

sshq is designed for tools that call SSH through subprocesses. It gives agents structured output by default while keeping terminal use readable for humans.

## Agent-First Principle

AI agents usually call command-line tools through pipes. They need stable fields, predictable errors, and stdout that can be parsed without filtering prompts or progress lines.

sshq treats that subprocess environment as a first-class use case:

- A pipe receives JSON by default.
- A terminal receives readable text by default.
- Connection status, progress, and verbose diagnostics go to stderr.
- Remote command results use a stable JSON envelope.

This lets an agent call `sshq myhost "hostname"` without adding a special output flag.

## TTY Auto-Detection

sshq resolves output mode in this order:

1. `--json`
2. `--pretty`
3. `SSHQ_OUTPUT=json`
4. stdout TTY detection

Terminal output uses pretty mode:

```bash
sshq myhost "hostname"
```

```text
myhost
```

Pipe output uses JSON mode:

```bash
result=$(sshq myhost "hostname")
```

```json
{"protocol":"sshq/3","exit_code":0,"data":{"stdout":"myhost\n","stderr":"","alias":"myhost","duration_ms":42}}
```

Force either mode when needed:

```bash
sshq --json myhost "hostname"
sshq --pretty myhost "hostname" | tee output.txt
```

> [!NOTE]
> `--json` and `--pretty` are persistent flags. Put them before the alias or before an explicit subcommand.

## JSON Envelope Contract

Every JSON response is one of two mutually exclusive envelope shapes. A `data` envelope means sshq completed the operation. An `error` envelope means sshq could not complete it.

Remote command success:

```json
{"protocol":"sshq/3","exit_code":0,"data":{"stdout":"myhost\n","stderr":"","alias":"myhost","duration_ms":42}}
```

Error:

```json
{"protocol":"sshq/3","error":{"code":"host_key_unknown","hint":"host key unknown for myhost (10.0.0.1:22)","action":"sshq trust myhost"}}
```

Agent callers should branch on the presence of `error`. For `exec`, a `data` envelope also carries the exact remote process result in top-level `exit_code` — see [Reading exit_code Correctly](#reading-exit_code-correctly) below.

Every envelope carries `protocol: "sshq/3"`. The machine-readable contract is published as a JSON Schema at [`schemas/envelope-v3.schema.json`](https://github.com/shayuc137/sshq/blob/main/schemas/envelope-v3.schema.json); validate against it instead of guessing field shapes.

## stdout Purity Guarantee

sshq keeps its own informational output out of stdout.

For `exec` in pretty mode, process stdout is exactly the remote stdout, while process stderr contains remote stderr plus sshq diagnostics. Status lines such as `connecting to myhost...` go to stderr.

For `exec` in JSON mode, process stdout contains the JSON envelope. `data.stdout` is exactly the remote stdout, `data.stderr` is the remote stderr, and status lines, progress, and verbose diagnostics go to stderr.

> [!TIP]
> Use stderr for logs and diagnostics in your agent integration. Treat stdout as the machine contract.

## exec JSON Result

Use `--json` when you want the structured result in a terminal:

```bash
sshq --json exec myhost "uname -a"
```

The full envelope for a successful exec:

```json
{
  "protocol": "sshq/3",
  "exit_code": 0,
  "data": {
    "stdout": "Linux myhost 6.8.0\n",
    "stderr": "",
    "alias": "myhost",
    "duration_ms": 42
  }
}
```

| Field | Meaning |
|-------|---------|
| `protocol` | Envelope contract version, always `sshq/3` for this release line |
| top-level `exit_code` | Exact remote process exit code; present only for a single remote command |
| `data.stdout` | Remote stdout, preserved exactly |
| `data.stderr` | Remote stderr |
| `data.alias` | sshq host alias |
| `data.duration_ms` | Remote command duration in milliseconds |

## Reading exit_code Correctly

A `data` envelope means sshq completed the call. For a single remote command, top-level `exit_code` is the exact remote result: `0` means success and any non-zero value means the remote command failed.

A remote command that fails with exit code 2:

```json
{"protocol":"sshq/3","exit_code":2,"data":{"stdout":"","stderr":"ls: cannot access '/nonexistent': No such file or directory\n","alias":"myhost","duration_ms":112}}
```

This response has `data` and `exit_code: 2`. The sshq call completed, while the remote `ls` failed.

The correct check:

```bash
json=$(sshq myhost "test -f /etc/os-release")
printf '%s' "$json" | jq -e 'has("data") and .exit_code == 0'
```

When sshq cannot complete the operation, the response carries `error` instead of `data`, and there is no top-level `exit_code`:

```json
{"protocol":"sshq/3","error":{"code":"host_not_found","hint":"host \"myhost\" not found","action":"run 'sshq ls' to see available hosts"}}
```

Summary:

| Envelope | Process exit | Meaning |
|----------|--------------|---------|
| `data`, exec `exit_code: 0` | `0` | Completed with a successful result |
| `data`, exec `exit_code` non-zero, or another unsuccessful result | `1` | Completed with an unsuccessful result |
| `error` | `2` | sshq could not complete the operation; branch on `error.code` |

## Suggested Error Actions

Text errors use a two-line format:

```text
Error: host key unknown for myhost (10.0.0.1:22)
  -> run: sshq trust myhost
```

JSON errors carry the same information:

```json
{"protocol":"sshq/3","error":{"code":"host_key_unknown","hint":"host key unknown for myhost (10.0.0.1:22)","action":"sshq trust myhost"}}
```

Common actions include:

- `run: sshq trust myhost`
- `if expected, run: sshq trust myhost --replace`
- `run 'sshq ls' to see available hosts`
- `retry or use --no-daemon`

Agents should branch programmatically on `error.code`, surface `error.hint` to users, and use `error.action` as the next command suggestion. Two codes ask for extra care before retrying: `result_indeterminate` means the operation may already have run, and `timeout` means the local `--timeout` deadline expired while the remote command may still be running — in both cases verify remote state instead of retrying blindly.

## Install as a Claude Code Skill

Install the bundled skill:

```bash
claude skill add --from github.com/shayuc137/sshq/skills/sshq
```

The skill tells Claude Code to route SSH operations through `sshq` as the command layer. Example requests map to commands like:

```bash
sshq myhost "uptime"
sshq cp ./deploy.tar.gz myhost:/tmp/
sshq probe myhost
```

## Best Practices for Agent Callers

Always quote remote commands:

```bash
sshq myhost "df -h"
sshq myhost "uname -a"
```

Use `--script-file` for complex scripts:

```bash
printf '%s\n' 'set -e' 'hostname' 'uptime' 'df -h' > /tmp/check-system.sh
sshq exec --script-file /tmp/check-system.sh myhost
```

Check both the envelope and the remote exit code:

```bash
json=$(sshq myhost "test -f /etc/os-release")
printf '%s' "$json" | jq -e 'has("data") and .exit_code == 0'
```

Use these flags for predictable automation:

```bash
sshq --no-progress cp ./app.tar.gz myhost:/tmp/
sshq --timeout 10s myhost "hostname"
sshq exec --no-daemon myhost "hostname"
```

Treat `data` and `error` as mutually exclusive. Only single remote command results contain top-level `exit_code`; cluster results keep per-host codes in `data.results[].exit_code`.

## Security-Sensitive Actions

Some operations require user confirmation or a controlling terminal. Agents should relay these to the user rather than attempting them autonomously:

- `sshq trust --replace <alias>` — overwrites a known host key; possible MITM
- `sshq credential set <alias>` — requires TTY for password input
- `sshq credential delete <alias>` — permanently deletes a stored password
- `sshq policy grant <alias> ...` — requires TTY; agents cannot self-grant
- `sshq config remove <alias>` — deletes a host from SSH config
- Remote forward (`-R`) — exposes local services to the remote network
- Destructive remote commands — `rm`, `shutdown`, `reboot`, `mkfs`, `systemctl stop`, firewall changes

When a command is blocked by policy, the error includes `error.action` with a suggested `policy grant` command. Relay this to the user as-is.
