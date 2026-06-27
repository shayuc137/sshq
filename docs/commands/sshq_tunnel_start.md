## sshq tunnel start

Start a port forwarding tunnel

### Synopsis

Start an SSH tunnel for port forwarding.

Examples:
  sshq tunnel start ali -L 8080:localhost:80     local forward
  sshq tunnel start ali -R 9090:localhost:3000    remote forward

```
sshq tunnel start <alias> [flags]
```

### Options

```
  -L, --L string   local forward: <local_port>:<remote_host>:<remote_port>
  -R, --R string   remote forward: <remote_port>:<local_host>:<local_port>
  -h, --help       help for start
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

* [sshq tunnel](sshq_tunnel.md)	 - Manage SSH port forwarding

