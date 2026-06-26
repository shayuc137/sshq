package cli

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/shayuc137/sshq/internal/exec"
	"github.com/shayuc137/sshq/internal/ipc"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/sshclient"
	"github.com/spf13/cobra"
)

func newExecCommand() *cobra.Command {
	cmd := &cobra.Command{
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

			noDaemon, _ := cmd.Flags().GetBool("no-daemon")
			w := writerFrom(cmd.Context())

			if !noDaemon && ipc.IsRunning() {
				return execViaDaemon(cmd, w, alias, command)
			}

			return execDirect(cmd, w, alias, command)
		},
	}

	cmd.Flags().Bool("no-daemon", false, "skip daemon, connect directly")
	return cmd
}

func execViaDaemon(cmd *cobra.Command, w *output.Writer, alias, command string) error {
	timeout, _ := cmd.Flags().GetDuration("timeout")

	conn, err := ipc.Connect()
	if err != nil {
		w.Info("daemon unreachable, falling back to direct connection")
		return execDirect(cmd, w, alias, command)
	}
	defer conn.Close()

	req := ipc.Request{
		Action:          "exec",
		ProtocolVersion: ipc.ProtocolVersion,
		Alias:           alias,
		Command:         command,
		Timeout:         int(timeout.Seconds()),
	}
	if err := ipc.Send(conn, req); err != nil {
		w.Info("daemon send failed, falling back to direct connection")
		return execDirect(cmd, w, alias, command)
	}

	stdout := cmd.OutOrStdout()
	stderr := cmd.ErrOrStderr()

	for {
		msg, err := ipc.Recv(conn)
		if err != nil {
			return output.Errorf("daemon connection lost", "retry or use --no-daemon")
		}

		var frame ipc.Frame
		if err := json.Unmarshal(msg, &frame); err != nil {
			return output.Errorf("invalid daemon response", "")
		}

		switch frame.Type {
		case "stdout":
			stdout.Write([]byte(frame.Data))
		case "stderr":
			stderr.Write([]byte(frame.Data))
		case "exit":
			if frame.Code != 0 {
				return &exec.ExitError{Code: frame.Code}
			}
			return nil
		case "error":
			return output.Errorf(frame.Hint, frame.Action)
		}
	}
}

func execDirect(cmd *cobra.Command, w *output.Writer, alias, command string) error {
	store := configFrom(cmd.Context())
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
}

func timeoutContext(parent context.Context, d time.Duration) (context.Context, func()) {
	if _, ok := parent.Deadline(); ok {
		return parent, func() {}
	}
	return context.WithTimeout(parent, d)
}
