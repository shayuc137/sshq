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
{"ok":true,"exit_code":0,"data":{"exit_code":0,"stdout":"myhost\n","stderr":"","host":"myhost","duration_ms":42},"schema_version":2}
```

Force either mode when needed:

```bash
sshq --json myhost "hostname"
sshq --pretty myhost "hostname" | tee output.txt
```

> [!NOTE]
> `--json` and `--pretty` are persistent flags. Put them before the alias or before an explicit subcommand.

## JSON Envelope Contract

Every JSON response is a single envelope with `schema_version`.

Remote command success:

```json
{"ok":true,"exit_code":0,"data":{"exit_code":0,"stdout":"myhost\n","stderr":"","host":"myhost","duration_ms":42},"schema_version":2}
```

Error:

```json
{"ok":false,"error":{"hint":"host key unknown for myhost (10.0.0.1:22)","action":"sshq trust myhost"},"schema_version":2}
```

Agent callers should branch on `ok` first. For `exec`, they must also check the top-level `exit_code` — see [Reading exit_code Correctly](#reading-exit_code-correctly) below.

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
  "ok": true,
  "exit_code": 0,
  "data": {
    "exit_code": 0,
    "stdout": "Linux myhost 6.8.0\n",
    "stderr": "",
    "host": "myhost",
    "duration_ms": 42
  },
  "schema_version": 2
}
```

| Field | Meaning |
|-------|---------|
| top-level `exit_code` | Remote process exit code — the field agents should read |
| `data.exit_code` | Same value, kept for compatibility |
| `data.stdout` | Remote stdout, preserved exactly |
| `data.stderr` | Remote stderr |
| `data.host` | sshq host alias |
| `data.duration_ms` | Remote command duration in milliseconds |

## Reading exit_code Correctly

`ok: true` means the sshq call itself completed — it connected, ran the command, and captured output. It does not mean the remote command succeeded. The remote result is in the top-level `exit_code`.

A remote command that fails with exit code 2:

```json
{"ok":true,"exit_code":2,"data":{"exit_code":2,"stdout":"","stderr":"ls: cannot access '/nonexistent': No such file or directory\n","host":"myhost","duration_ms":112},"schema_version":2}
```

This response has `ok: true` and `exit_code: 2`. The sshq call worked, but the remote `ls` failed. An agent that treats `ok: true` as success would miss the error.

The correct check:

```bash
json=$(sshq myhost "test -f /etc/os-release")
printf '%s' "$json" | jq -e '.ok == true and .exit_code == 0'
```

When `ok` is `false`, the response carries an `error` object instead of `data`, and there is no top-level `exit_code`:

```json
{"ok":false,"error":{"hint":"host \"myhost\" not found","action":"run 'sshq ls' to see available hosts"},"schema_version":2}
```

Summary:

| `ok` | `exit_code` | Meaning |
|------|-------------|---------|
| `true` | `0` | Remote command succeeded |
| `true` | non-zero | Remote command failed (sshq call was fine) |
| `false` | absent | sshq-level failure — see `error.hint` |

## Suggested Error Actions

Text errors use a two-line format:

```text
Error: host key unknown for myhost (10.0.0.1:22)
  -> run: sshq trust myhost
```

JSON errors carry the same information:

```json
{"ok":false,"error":{"hint":"host key unknown for myhost (10.0.0.1:22)","action":"sshq trust myhost"},"schema_version":2}
```

Common actions include:

- `run: sshq trust myhost`
- `if expected, run: sshq trust myhost --replace`
- `run 'sshq ls' to see available hosts`
- `retry or use --no-daemon`

Agents can surface `error.hint` to users and use `error.action` as the next command suggestion.

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
printf '%s' "$json" | jq -e '.ok == true and .exit_code == 0'
```

Use these flags for predictable automation:

```bash
sshq --no-progress cp ./app.tar.gz myhost:/tmp/
sshq --timeout 10s myhost "hostname"
sshq exec --no-daemon myhost "hostname"
```

Treat `schema_version` as the compatibility gate for parsers.

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
