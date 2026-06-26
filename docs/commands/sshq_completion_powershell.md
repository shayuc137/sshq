## sshq completion powershell

Generate the autocompletion script for powershell

### Synopsis

Generate the autocompletion script for powershell.

To load completions in your current shell session:

	sshq completion powershell | Out-String | Invoke-Expression

To load completions for every new session, add the output of the above command
to your powershell profile.


```
sshq completion powershell [flags]
```

### Options

```
  -h, --help              help for powershell
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

