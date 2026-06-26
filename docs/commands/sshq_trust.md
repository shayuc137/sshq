## sshq trust

Fetch and trust a host's SSH key

### Synopsis

Fetch the SSH host key from a remote server and add it to known_hosts.
If the key has changed (mismatch), use --replace to update it.

```
sshq trust [alias] [flags]
```

### Options

```
      --all       trust all configured hosts
  -h, --help      help for trust
      --replace   replace mismatched host keys
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

