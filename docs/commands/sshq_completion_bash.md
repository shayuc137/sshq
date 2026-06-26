## sshq completion bash

Generate the autocompletion script for bash

### Synopsis

Generate the autocompletion script for the bash shell.

This script depends on the 'bash-completion' package.
If it is not installed already, you can install it via your OS's package manager.

To load completions in your current shell session:

	source <(sshq completion bash)

To load completions for every new session, execute once:

#### Linux:

	sshq completion bash > /etc/bash_completion.d/sshq

#### macOS:

	sshq completion bash > $(brew --prefix)/etc/bash_completion.d/sshq

You will need to start a new shell for this setup to take effect.


```
sshq completion bash
```

### Options

```
  -h, --help              help for bash
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

