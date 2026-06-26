# sshq

Agent-native SSH multiplexing CLI. Single binary, cross-platform.

## Install

```bash
go install github.com/shayuc137/sshq/cmd/sshq@latest
```

Or download from [GitHub Releases](https://github.com/shayuc137/sshq/releases).

## Commands

| Command | Description | Status |
|---------|-------------|--------|
| `sshq version` | Print version information | Available |
| `sshq exec <alias> <cmd>` | Execute a command on a remote host | Available |
| `sshq ls` | List configured SSH hosts | Available |
| `sshq search <pattern>` | Search SSH hosts by pattern | Available |
| `sshq info <alias>` | Show detailed host information | Available |
| `sshq probe <alias>` | Check TCP connectivity to a host | Available |
| `sshq probe --all` | Probe all configured hosts | Available |
| `sshq daemon start` | Start the connection pool daemon | Available |
| `sshq daemon stop` | Stop the daemon | Available |
| `sshq daemon status` | Show daemon status | Available |
| `sshq cp` | Copy files between local and remote hosts | Phase 2 |
| `sshq config` | Manage sshq configuration | Phase 1 |

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
