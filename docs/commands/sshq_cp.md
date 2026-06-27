## sshq cp

Copy files between local and remote hosts

### Synopsis

Copy files using alias:path syntax to determine direction:
  sshq cp local.txt ali:/tmp/          upload
  sshq cp ali:/var/log/app.log ./      download
  sshq cp ali:/data/f.tar rn:/backup/  server-to-server relay

```
sshq cp <src> <dst> [flags]
```

### Options

```
  -h, --help          help for cp
      --no-daemon     skip daemon, connect directly
      --no-progress   disable progress output
  -r, --recursive     copy directories recursively
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

