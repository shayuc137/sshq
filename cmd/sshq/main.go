package main

import (
	"context"
	"errors"
	"os"

	"github.com/shayuc137/sshq/internal/cli"
	"github.com/shayuc137/sshq/internal/output"
)

func main() {
	ctx := context.Background()
	cmd := cli.NewRootCommand()

	if err := cmd.ExecuteContext(ctx); err != nil {
		w := output.New(cmd.OutOrStdout(), cmd.ErrOrStderr())
		if (cmd.Flag("json") != nil && cmd.Flag("json").Changed) || output.DetectEnvJSONMode() {
			w.SetJSONMode(true)
		}

		var cmdErr *output.CmdError
		if errors.As(err, &cmdErr) {
			w.RenderError(cmdErr)
		} else {
			w.RenderError(output.Errorf(err.Error(), ""))
		}
		os.Exit(1)
	}
}
