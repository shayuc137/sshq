package cli

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"strings"
	"time"

	"github.com/shayuc137/sshq/internal/exec"
	"github.com/shayuc137/sshq/internal/ipc"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/remote"
	"github.com/shayuc137/sshq/internal/sshclient"
	"github.com/spf13/cobra"
)

func newExecCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exec <alias> <command...>",
		Short: "Execute a command on a remote host",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExecCommand(cmd, args)
		},
	}

	cmd.Flags().Bool("no-daemon", false, "skip daemon, connect directly")
	cmd.Flags().String("script-file", "", "execute a local script file on the remote host via stdin")
	cmd.Flags().String("shell", "", "override detected remote shell type (bash/ash/zsh/sh/powershell)")
	return cmd
}

func runExecCommand(cmd *cobra.Command, args []string) error {
	alias := args[0]

	store := configFrom(cmd.Context())
	if store == nil {
		return output.Errorf("no SSH config loaded", "check ~/.ssh/config exists")
	}

	w := writerFrom(cmd.Context())

	scriptFile, _ := cmd.Flags().GetString("script-file")
	if scriptFile != "" {
		return execScript(cmd, w, alias, scriptFile)
	}

	if len(args) < 2 {
		return output.Errorf("command required", "usage: sshq exec <alias> <command...> or sshq exec --script-file <path> <alias>")
	}
	command := strings.Join(args[1:], " ")

	noDaemon, _ := cmd.Flags().GetBool("no-daemon")
	if !noDaemon && ipc.IsRunning() {
		return execViaDaemon(cmd, w, alias, command)
	}

	return execDirect(cmd, w, alias, command)
}

func execScript(cmd *cobra.Command, w *output.Writer, alias, scriptFile string) error {
	script, err := os.ReadFile(scriptFile)
	if err != nil {
		return output.Errorf("read script file: "+err.Error(), "check file path")
	}

	noDaemon, _ := cmd.Flags().GetBool("no-daemon")
	if !noDaemon && ipc.IsRunning() {
		return execScriptViaDaemon(cmd, w, alias, script)
	}

	return execScriptDirect(cmd, w, alias, script)
}

func execScriptViaDaemon(cmd *cobra.Command, w *output.Writer, alias string, script []byte) error {
	timeout, _ := cmd.Flags().GetDuration("timeout")
	shellOverride := execShellOverride(cmd)

	conn, err := ipc.Connect()
	if err != nil {
		w.Info("daemon unreachable, falling back to direct connection")
		return execScriptDirect(cmd, w, alias, script)
	}
	defer conn.Close()

	env, _ := ipc.MakeEnvelope("script", ipc.ScriptPayload{
		Alias:   alias,
		Script:  script,
		Shell:   shellOverride,
		Timeout: int(timeout.Seconds()),
	})
	if err := ipc.Send(conn, env); err != nil {
		w.Info("daemon send failed, falling back to direct connection")
		return execScriptDirect(cmd, w, alias, script)
	}

	return recvExecFrames(w, conn, alias)
}

func execScriptDirect(cmd *cobra.Command, w *output.Writer, alias string, script []byte) error {
	store := configFrom(cmd.Context())
	host, err := store.Get(alias)
	if err != nil {
		return output.Errorf(err.Error(), "run 'sshq ls' to see available hosts")
	}

	timeout, _ := cmd.Flags().GetDuration("timeout")
	cfg := hostToConnConfigWithStore(host, store)
	cfg.Timeout = timeout

	w.Info("connecting to " + alias + "...")
	ctx := cmd.Context()
	if timeout > 0 {
		var cancel func()
		ctx, cancel = timeoutContext(ctx, timeout)
		defer cancel()
	}

	client, err := sshclient.Dial(ctx, cfg)
	if err != nil {
		return connErrorToOutput(err, alias)
	}
	defer client.Close()

	cache := profileCacheFrom(ctx)
	profile, _ := remote.GetProfile(ctx, client, cache, host.HostName, host.Port)
	shell := shellForExec(profile, execShellOverride(cmd))

	w.Info("executing script via " + shell + "...")

	start := time.Now()
	result, err := exec.RunScriptBuffered(ctx, client, script, shell)
	if err != nil {
		return output.Errorf(err.Error(), "")
	}
	if remote.NeedsTranscoding(profile) {
		result.Stdout = remote.DecodeString(result.Stdout, profile.Encoding)
		result.Stderr = remote.DecodeString(result.Stderr, profile.Encoding)
	}
	w.Exec(&output.ExecResult{
		ExitCode:   result.ExitCode,
		Stdout:     result.Stdout,
		Stderr:     result.Stderr,
		Host:       alias,
		DurationMs: time.Since(start).Milliseconds(),
	})
	if result.ExitCode != 0 {
		return &exec.ExitError{Code: result.ExitCode}
	}
	return nil
}

func execViaDaemon(cmd *cobra.Command, w *output.Writer, alias, command string) error {
	timeout, _ := cmd.Flags().GetDuration("timeout")
	shellOverride := execShellOverride(cmd)

	conn, err := ipc.Connect()
	if err != nil {
		w.Info("daemon unreachable, falling back to direct connection")
		return execDirect(cmd, w, alias, command)
	}
	defer conn.Close()

	env, _ := ipc.MakeEnvelope("exec", ipc.ExecPayload{
		Alias:   alias,
		Command: command,
		Shell:   shellOverride,
		Timeout: int(timeout.Seconds()),
	})
	if err := ipc.Send(conn, env); err != nil {
		w.Info("daemon send failed, falling back to direct connection")
		return execDirect(cmd, w, alias, command)
	}

	return recvExecFrames(w, conn, alias)
}

func recvExecFrames(w *output.Writer, conn net.Conn, alias string) error {
	var stdoutBuf, stderrBuf strings.Builder
	start := time.Now()

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
			stdoutBuf.WriteString(frame.Data)
		case "stderr":
			stderrBuf.WriteString(frame.Data)
		case "exit":
			w.Exec(&output.ExecResult{
				ExitCode:   frame.Code,
				Stdout:     stdoutBuf.String(),
				Stderr:     stderrBuf.String(),
				Host:       alias,
				DurationMs: time.Since(start).Milliseconds(),
			})
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
	cfg := hostToConnConfigWithStore(host, store)
	cfg.Timeout = timeout

	w.Info("connecting to " + alias + "...")

	ctx := cmd.Context()
	if timeout > 0 {
		var cancel func()
		ctx, cancel = timeoutContext(ctx, timeout)
		defer cancel()
	}

	client, err := sshclient.Dial(ctx, cfg)
	if err != nil {
		return connErrorToOutput(err, alias)
	}
	defer client.Close()

	cache := profileCacheFrom(ctx)
	profile, _ := remote.GetProfile(ctx, client, cache, host.HostName, host.Port)
	shell := shellForExec(profile, execShellOverride(cmd))

	start := time.Now()
	result, err := exec.RunBufferedWithShell(ctx, client, command, shell)
	if err != nil {
		return output.Errorf(err.Error(), "")
	}
	if remote.NeedsTranscoding(profile) {
		result.Stdout = remote.DecodeString(result.Stdout, profile.Encoding)
		result.Stderr = remote.DecodeString(result.Stderr, profile.Encoding)
	}
	w.Exec(&output.ExecResult{
		ExitCode:   result.ExitCode,
		Stdout:     result.Stdout,
		Stderr:     result.Stderr,
		Host:       alias,
		DurationMs: time.Since(start).Milliseconds(),
	})
	if result.ExitCode != 0 {
		return &exec.ExitError{Code: result.ExitCode}
	}
	return nil
}

func execShellOverride(cmd *cobra.Command) string {
	shell, _ := cmd.Flags().GetString("shell")
	return shell
}

func shellForExec(profile *remote.Profile, override string) string {
	if override != "" {
		return override
	}
	if profile == nil {
		return ""
	}
	return string(profile.Shell)
}

func timeoutContext(parent context.Context, d time.Duration) (context.Context, func()) {
	if _, ok := parent.Deadline(); ok {
		return parent, func() {}
	}
	return context.WithTimeout(parent, d)
}
