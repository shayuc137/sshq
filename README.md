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
| `sshq exec` | Execute a command on a remote host | Phase 1 |
| `sshq cp` | Copy files between local and remote hosts | Phase 2 |
| `sshq ls` | List configured SSH hosts | Phase 1 |
| `sshq search` | Search SSH hosts by pattern | Phase 1 |
| `sshq info` | Show detailed host information | Phase 1 |
| `sshq probe` | Check TCP connectivity to a host | Phase 1 |
| `sshq daemon` | Manage the connection pool daemon | Phase 1 |
| `sshq config` | Manage sshq configuration | Phase 1 |

## Global Flags

| Flag | Description |
|------|-------------|
| `--json` | Output in JSON format |
| `--verbose` | Verbose output |
| `--timeout` | Operation timeout (default: 30s) |

JSON mode can also be enabled via `SSHQ_OUTPUT=json` environment variable.

## License

[MIT](LICENSE)
