## sshq config add

Add a new SSH host

```
sshq config add <alias> [flags]
```

### Options

```
      --desc string         host description
      --env string          environment identifier
  -h, --help                help for add
      --hostname string     remote hostname or IP (required)
      --identity string     identity file path
      --port string         SSH port
      --proxy-jump string   ProxyJump host
      --tag string          comma-separated tags
      --user string         SSH user
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

* [sshq config](sshq_config.md)	 - Manage SSH host configuration

