package cli

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"strings"
	"time"

	"github.com/shayuc137/sshq/internal/audit"
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
		Example: `sshq exec myhost "uname -a"
sshq exec myhost --script-file ./deploy.sh --shell bash --no-daemon
sshq exec windows-host --script-file ./diagnose.ps1 --shell powershell`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExecCommand(cmd, args)
		},
	}

	registerExecFlags(cmd)
	return cmd
}

func registerExecFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("no-daemon", false, "skip daemon, connect directly")
	cmd.Flags().String("script-file", "", "execute a local script file on the remote host")
	cmd.Flags().String("shell", "", "override detected remote shell type (bash/ash/zsh/sh/powershell)")
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
		timeout, _ := cmd.Flags().GetDuration("timeout")
		env, _ := ipc.MakeEnvelope("exec", ipc.ExecPayload{
			Alias:   alias,
			Command: command,
			Shell:   execShellOverride(cmd),
			Timeout: int(timeout.Seconds()),
			Verbose: w.IsVerbose(),
		})
		return daemonDispatch(env,
			func(conn net.Conn) error {
				return recvExecFrames(w, conn, alias)
			},
			func(reason string) error {
				w.Info(reason + ", falling back to direct connection")
				return execDirect(cmd, w, alias, command)
			},
		)
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
		timeout, _ := cmd.Flags().GetDuration("timeout")
		env, _ := ipc.MakeEnvelope("script", ipc.ScriptPayload{
			Alias:   alias,
			Script:  script,
			Shell:   execShellOverride(cmd),
			Timeout: int(timeout.Seconds()),
			Verbose: w.IsVerbose(),
		})
		return daemonDispatch(env,
			func(conn net.Conn) error {
				return recvExecFrames(w, conn, alias)
			},
			func(reason string) error {
				w.Info(reason + ", falling back to direct connection")
				return execScriptDirect(cmd, w, alias, script)
			},
		)
	}

	return execScriptDirect(cmd, w, alias, script)
}

func execScriptDirect(cmd *cobra.Command, w *output.Writer, alias string, script []byte) error {
	if err := checkPolicyCommandWithAudit(cmd.Context(), alias, string(script), audit.OperationExec, audit.ScriptSummary(script), audit.SourceDirect); err != nil {
		return err
	}

	store := configFrom(cmd.Context())
	host, err := store.Get(alias)
	if err != nil {
		entry := audit.ScriptErrorEntry(alias, script, audit.ResultError, 0, audit.SourceDirect, err)
		if auditErr := recordAudit(cmd.Context(), entry); auditErr != nil {
			return auditErr
		}
		return output.Errorf(err.Error(), "run 'sshq ls' to see available hosts")
	}

	timeout, _ := cmd.Flags().GetDuration("timeout")
	cfg, err := hostToConnConfigWithCredentials(host, store, credentialStoreFrom(cmd.Context()))
	if err != nil {
		return credentialOutputError(err, alias)
	}
	cfg.Timeout = timeout

	w.Verbose("connecting to " + alias + "...")
	ctx := cmd.Context()
	if timeout > 0 {
		var cancel func()
		ctx, cancel = timeoutContext(ctx, timeout)
		defer cancel()
	}

	connectStart := time.Now()
	client, err := sshclient.Dial(ctx, cfg)
	if err != nil {
		entry := audit.ScriptErrorEntry(alias, script, audit.ResultError, 0, audit.SourceDirect, err)
		if auditErr := recordAudit(ctx, entry); auditErr != nil {
			return auditErr
		}
		return connErrorToOutput(err, alias)
	}
	defer client.Close()
	w.Verbose("connection: alias=" + alias + " duration=" + verboseDuration(time.Since(connectStart)) + " direct")

	cache := profileCacheFrom(ctx)
	profile, profileErr := remote.GetProfile(ctx, client, cache, host.HostName, host.Port)
	if profileErr != nil {
		w.Verbose("shell detection warning: " + profileErr.Error())
	}
	w.Verbose(verboseProfile(profile))
	shell := shellForExec(profile, execShellOverride(cmd))
	if shell == "" {
		w.Verbose("shell selected: default")
	} else {
		w.Verbose("shell selected: " + shell)
	}

	w.Info("executing script via " + shell + "...")

	start := time.Now()
	result, err := exec.RunScriptBuffered(ctx, client, script, shell,
		exec.WithRemoteProfile(profile),
		exec.WithScriptVerbose(w.Verbose),
	)
	durationMs := time.Since(start).Milliseconds()
	if err != nil {
		if auditErr := recordAudit(ctx, audit.ScriptErrorEntry(alias, script, audit.ResultError, durationMs, audit.SourceDirect, err)); auditErr != nil {
			return auditErr
		}
		return output.Errorf(err.Error(), "")
	}
	normalizeRemoteResult(result, profile, shell)
	auditResult := audit.ResultSuccess
	if result.ExitCode != 0 {
		auditResult = audit.ResultError
	}
	if err := recordAudit(ctx, audit.ScriptEntry(alias, script, auditResult, result.ExitCode, durationMs, audit.SourceDirect)); err != nil {
		return err
	}
	w.Exec(&output.ExecResult{
		ExitCode:   result.ExitCode,
		Stdout:     result.Stdout,
		Stderr:     result.Stderr,
		Host:       alias,
		DurationMs: durationMs,
	})
	if result.ExitCode != 0 {
		return &exec.ExitError{Code: result.ExitCode}
	}
	return nil
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
		case daemonVerboseFrame:
			recvVerboseFrame(w, frame)
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
	if err := checkPolicyCommand(cmd.Context(), alias, command); err != nil {
		return err
	}

	store := configFrom(cmd.Context())
	host, err := store.Get(alias)
	if err != nil {
		entry := audit.ExecErrorEntry(alias, command, audit.ResultError, 0, audit.SourceDirect, err)
		if auditErr := recordAudit(cmd.Context(), entry); auditErr != nil {
			return auditErr
		}
		return output.Errorf(err.Error(), "run 'sshq ls' to see available hosts")
	}

	timeout, _ := cmd.Flags().GetDuration("timeout")
	cfg, err := hostToConnConfigWithCredentials(host, store, credentialStoreFrom(cmd.Context()))
	if err != nil {
		return credentialOutputError(err, alias)
	}
	cfg.Timeout = timeout

	w.Verbose("connecting to " + alias + "...")

	ctx := cmd.Context()
	if timeout > 0 {
		var cancel func()
		ctx, cancel = timeoutContext(ctx, timeout)
		defer cancel()
	}

	connectStart := time.Now()
	client, err := sshclient.Dial(ctx, cfg)
	if err != nil {
		entry := audit.ExecErrorEntry(alias, command, audit.ResultError, 0, audit.SourceDirect, err)
		if auditErr := recordAudit(ctx, entry); auditErr != nil {
			return auditErr
		}
		return connErrorToOutput(err, alias)
	}
	defer client.Close()
	w.Verbose("connection: alias=" + alias + " duration=" + verboseDuration(time.Since(connectStart)) + " direct")

	cache := profileCacheFrom(ctx)
	profile, profileErr := remote.GetProfile(ctx, client, cache, host.HostName, host.Port)
	if profileErr != nil {
		w.Verbose("shell detection warning: " + profileErr.Error())
	}
	w.Verbose(verboseProfile(profile))
	shellOverride := execShellOverride(cmd)
	shell := shellForExec(profile, shellOverride)
	if shell == "" {
		w.Verbose("shell selected: default")
	} else {
		w.Verbose("shell selected: " + shell)
	}

	start := time.Now()
	result, err := exec.RunBufferedWithShell(ctx, client, command, shell)
	durationMs := time.Since(start).Milliseconds()
	if err != nil {
		if auditErr := recordAudit(ctx, audit.ExecErrorEntry(alias, command, audit.ResultError, durationMs, audit.SourceDirect, err)); auditErr != nil {
			return auditErr
		}
		return output.Errorf(err.Error(), "")
	}
	staleProfile := shellOverride == "" && invalidateSuspectedStaleProfile(cache, host.HostName, host.Port, profile, result)
	if staleProfile {
		w.Verbose("shell profile invalidated: alias=" + alias)
	}
	normalizeRemoteResult(result, profile, shell)
	if staleProfile {
		result.Stderr = appendStaleProfileHint(result.Stderr, alias)
	}
	auditResult := audit.ResultSuccess
	if result.ExitCode != 0 {
		auditResult = audit.ResultError
	}
	if err := recordAudit(ctx, audit.ExecEntry(alias, command, auditResult, result.ExitCode, durationMs, audit.SourceDirect)); err != nil {
		return err
	}
	w.Exec(&output.ExecResult{
		ExitCode:   result.ExitCode,
		Stdout:     result.Stdout,
		Stderr:     result.Stderr,
		Host:       alias,
		DurationMs: durationMs,
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

func normalizeRemoteResult(result *exec.Result, profile *remote.Profile, shell string) {
	if remote.NeedsTranscoding(profile) {
		result.Stdout = remote.DecodeString(result.Stdout, profile.Encoding)
		result.Stderr = remote.DecodeString(result.Stderr, profile.Encoding)
	}
	result.Stderr = exec.DecodeCLIXMLStderr(result.Stderr)
	result.Stderr = appendPowerShellVariableHint(result.Stderr, shell, result.ExitCode)
}

func invalidateSuspectedStaleProfile(cache *remote.Cache, host, port string, profile *remote.Profile, result *exec.Result) bool {
	if result == nil || result.ExitCode == 0 || !remote.SuspectStaleProfile(profile, result.Stdout, result.Stderr) {
		return false
	}
	if cache != nil {
		cache.Invalidate(host, port)
	}
	return true
}

func appendStaleProfileHint(stderr, alias string) string {
	if stderr != "" && !strings.HasSuffix(stderr, "\n") {
		stderr += "\n"
	}
	return stderr + "shell profile was stale and has been invalidated; retry the command or run 'sshq doctor " + alias + "'\n"
}

const powerShellVariableHint = "if your command contained PowerShell $variables, the local shell may have expanded them — use --script-file"

func appendPowerShellVariableHint(stderr, shell string, exitCode int) string {
	if exitCode == 0 || !isPowerShellShell(shell) || strings.Contains(stderr, powerShellVariableHint) {
		return stderr
	}
	for _, marker := range []string{"ParserError", "TerminatorExpectedAtEndOfString", "Variable is not set"} {
		if strings.Contains(stderr, marker) {
			if stderr != "" && !strings.HasSuffix(stderr, "\n") {
				stderr += "\n"
			}
			return stderr + powerShellVariableHint + "\n"
		}
	}
	return stderr
}

func isPowerShellShell(shell string) bool {
	switch strings.ToLower(strings.TrimSpace(shell)) {
	case "powershell", "powershell.exe", "pwsh", "pwsh.exe":
		return true
	default:
		return false
	}
}

func timeoutContext(parent context.Context, d time.Duration) (context.Context, func()) {
	if _, ok := parent.Deadline(); ok {
		return parent, func() {}
	}
	return context.WithTimeout(parent, d)
}
