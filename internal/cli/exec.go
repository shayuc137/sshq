package cli

import (
	"context"
	"strings"
	"time"

	"github.com/shayuc137/sshq/internal/exec"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/sshclient"
	"github.com/spf13/cobra"
)

func newExecCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "exec <alias> <command...>",
		Short: "Execute a command on a remote host",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			alias := args[0]
			command := strings.Join(args[1:], " ")

			store := configFrom(cmd.Context())
			if store == nil {
				return output.Errorf("no SSH config loaded", "check ~/.ssh/config exists")
			}

			host, err := store.Get(alias)
			if err != nil {
				return output.Errorf(err.Error(), "run 'sshq ls' to see available hosts")
			}

			timeout, _ := cmd.Flags().GetDuration("timeout")
			cfg := sshclient.ConnConfig{
				Host:         host.HostName,
				Port:         host.Port,
				User:         host.User,
				IdentityFile: host.IdentityFile,
				Timeout:      timeout,
			}

			w := writerFrom(cmd.Context())
			w.Info("connecting to " + alias + "...")

			ctx := cmd.Context()
			if timeout > 0 {
				var cancel func()
				ctx, cancel = timeoutContext(ctx, timeout)
				defer cancel()
			}

			client, err := sshclient.Dial(ctx, cfg)
			if err != nil {
				return output.Errorf(err.Error(), "check connectivity and credentials")
			}
			defer client.Close()

			if w.IsJSONMode() {
				result, err := exec.RunBuffered(ctx, client, command)
				if err != nil {
					return output.Errorf(err.Error(), "")
				}
				w.JSONOut(result)
				if result.ExitCode != 0 {
					return &exec.ExitError{Code: result.ExitCode}
				}
				return nil
			}

			code, err := exec.Run(ctx, client, command, cmd.OutOrStdout(), cmd.ErrOrStderr())
			if err != nil {
				return output.Errorf(err.Error(), "")
			}
			if code != 0 {
				return &exec.ExitError{Code: code}
			}
			return nil
		},
	}
}

func timeoutContext(parent context.Context, d time.Duration) (context.Context, func()) {
	if _, ok := parent.Deadline(); ok {
		return parent, func() {}
	}
	return context.WithTimeout(parent, d)
}
