## sshq completion fish

Generate the autocompletion script for fish

### Synopsis

Generate the autocompletion script for the fish shell.

To load completions in your current shell session:

	sshq completion fish | source

To load completions for every new session, execute once:

	sshq completion fish > ~/.config/fish/completions/sshq.fish

You will need to start a new shell for this setup to take effect.


```
sshq completion fish [flags]
```

### Options

```
  -h, --help              help for fish
      --no-descriptions   disable completion descriptions
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

* [sshq completion](sshq_completion.md)	 - Generate the autocompletion script for the specified shell

