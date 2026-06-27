# sshq

Agent-native SSH multiplexing CLI. Single binary, cross-platform.

## Install

```bash
go install github.com/shayuc137/sshq/cmd/sshq@latest
```

Or download from [GitHub Releases](https://github.com/shayuc137/sshq/releases).

## Commands

| Command | Description |
|---------|-------------|
| `sshq exec <alias> <cmd>` | Execute a command on a remote host |
| `sshq exec --script-file <path> <alias>` | Execute a local script on the remote host |
| `sshq cp <src> <dst>` | Copy files (upload/download/relay) |
| `sshq ls` | List configured SSH hosts |
| `sshq search <pattern>` | Search SSH hosts by pattern |
| `sshq info <alias>` | Show detailed host information |
| `sshq probe <alias>` | Check TCP connectivity to a host |
| `sshq config add/set/remove` | Manage SSH host configuration |
| `sshq cluster exec` | Execute on multiple hosts concurrently |
| `sshq tunnel start/list/stop` | SSH port forwarding (local/remote) |
| `sshq daemon start/stop/status` | Connection pool daemon |
| `sshq trust <alias>` | Fetch and trust a host's SSH key |
| `sshq version` | Print version information |

Full command reference: [docs/commands/](docs/commands/)

## Global Flags

| Flag | Description |
|------|-------------|
| `--json` | Output in JSON format |
| `--pretty` | Human-readable output |
| `--config` | SSH config file path |
| `--verbose` | Verbose output |
| `--timeout` | Operation timeout (default: 30s) |

Default output is compact (agent-friendly). Use `--pretty` for human-readable format.

JSON mode can also be enabled via `SSHQ_OUTPUT=json` environment variable.

## License

[MIT](LICENSE)
