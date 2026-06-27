## sshq exec

Execute a command on a remote host

```
sshq exec <alias> <command...> [flags]
```

### Options

```
  -h, --help                 help for exec
      --no-daemon            skip daemon, connect directly
      --script-file string   execute a local script file on the remote host via stdin
      --shell string         override detected remote shell type (bash/ash/zsh/sh/powershell)
```

### Options inherited from parent commands

```
      --config string      SSH config file path
      --json               output in JSON format
      --pretty             human-readable output
      --timeout duration   operation timeout (default 30s)
  -v, --verbose            verbose output
```

### SEE ALSO

* [sshq](sshq.md)	 - Agent-native SSH multiplexing CLI

