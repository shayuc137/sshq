## sshq config set

Set a host property or metadata

### Synopsis

Set SSH properties (hostname, user, port, identityfile, proxyjump)
or sshq metadata (tags, env, description) on an existing host.

Examples:
  sshq config set myhost hostname 10.0.0.1
  sshq config set myhost tags prod,web
  sshq config set myhost description "production web server"

```
sshq config set <alias> <key> <value> [flags]
```

### Options

```
  -h, --help   help for set
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

