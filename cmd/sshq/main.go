package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/shayuc137/sshq/internal/cli"
	"github.com/shayuc137/sshq/internal/exec"
	"github.com/shayuc137/sshq/internal/output"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	cmd := cli.NewRootCommand()

	if err := cmd.ExecuteContext(ctx); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}

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
