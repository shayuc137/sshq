## sshq cluster exec

Execute a command on multiple hosts concurrently

```
sshq cluster exec <command> [flags]
```

### Options

```
      --all               target all configured hosts
      --concurrency int   max concurrent connections (default 10)
      --env string        filter hosts by environment
  -h, --help              help for exec
      --hosts string      comma-separated host aliases
      --no-daemon         skip daemon, connect directly
      --tag string        filter hosts by tag
```

### Options inherited from parent commands

```
      --config string      SSH config file path
      --json               output in JSON format
      --no-progress        disable progress output
      --pretty             human-readable output
      --timeout duration   operation timeout (default 30s)
  -v, --verbose            verbose output
```

### SEE ALSO

* [sshq cluster](sshq_cluster.md)	 - Concurrent operations across multiple hosts

