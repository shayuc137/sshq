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
			os.Exit(processExitCode(err))
		}

		var opts []output.Option
		if cmd.Flag("json") != nil && cmd.Flag("json").Changed {
			opts = append(opts, output.WithJSON())
		}
		if cmd.Flag("pretty") != nil && cmd.Flag("pretty").Changed {
			opts = append(opts, output.WithPretty())
		}
		w := output.New(cmd.OutOrStdout(), cmd.ErrOrStderr(), opts...)

		var cmdErr *output.CmdError
		if errors.As(err, &cmdErr) {
			w.Error(cmdErr)
			os.Exit(processExitCode(err))
		}

		var badNews *output.BadNewsError
		if errors.As(err, &badNews) {
			os.Exit(processExitCode(err))
		}

		w.Error(output.Errorf(err.Error(), "").WithCode("internal_error"))
		os.Exit(processExitCode(err))
	}
}

func processExitCode(err error) int {
	var remoteExit *exec.ExitError
	if errors.As(err, &remoteExit) {
		return 1
	}

	var coded interface{ ProcessExitCode() int }
	if errors.As(err, &coded) {
		return coded.ProcessExitCode()
	}
	return 2
}
