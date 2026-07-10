package exec

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/shayuc137/sshq/internal/powershell"
	"github.com/shayuc137/sshq/internal/remote"
	"github.com/shayuc137/sshq/internal/sshclient"
	"github.com/shayuc137/sshq/internal/transfer"
	"golang.org/x/crypto/ssh"
)

const (
	powerShellInlineLimit = 8 * 1024
	powerShellPrefix      = powershell.Prefix
)

type scriptOptions struct {
	profile *remote.Profile
	verbose func(string)
	ops     powerShellScriptOps
}

// ScriptOption supplies remote details needed by platform-specific script execution.
type ScriptOption func(*scriptOptions)

// WithRemoteProfile supplies the detected remote profile for upload-run fallback.
func WithRemoteProfile(profile *remote.Profile) ScriptOption {
	return func(opts *scriptOptions) { opts.profile = profile }
}

// WithScriptVerbose routes script execution diagnostics to the caller's verbose channel.
func WithScriptVerbose(verbose func(string)) ScriptOption {
	return func(opts *scriptOptions) { opts.verbose = verbose }
}

func withPowerShellScriptOps(ops powerShellScriptOps) ScriptOption {
	return func(opts *scriptOptions) { opts.ops = ops }
}

type powerShellScriptOps interface {
	Run(context.Context, string) (*Result, error)
	Upload(context.Context, string, string) error
	Remove(context.Context, string) error
}

type sshPowerShellScriptOps struct {
	client  *sshclient.Client
	profile *remote.Profile
	verbose func(string)
}

func (o *sshPowerShellScriptOps) Run(ctx context.Context, command string) (*Result, error) {
	return RunBuffered(ctx, o.client, command)
}

func (o *sshPowerShellScriptOps) Upload(ctx context.Context, localPath, remotePath string) error {
	engine, err := transfer.NewEngine(o.client, o.profile, o.verbose)
	if err != nil {
		return fmt.Errorf("create transfer engine: %w", err)
	}
	defer engine.Close()

	if _, err := engine.Upload(ctx, localPath, remotePath, nil); err != nil {
		return fmt.Errorf("upload PowerShell script: %w", err)
	}
	return nil
}

func (o *sshPowerShellScriptOps) Remove(ctx context.Context, remotePath string) error {
	cleanup := powerShellEncodedCommand([]byte(
		"Remove-Item -LiteralPath " + powerShellQuote(remotePath) + " -Force -ErrorAction Stop",
	))
	result, err := RunBuffered(ctx, o.client, cleanup)
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("remote cleanup exited %d: %s", result.ExitCode, strings.TrimSpace(result.Stderr))
	}
	return nil
}

func InterpreterCmd(shell string, script []byte) (string, error) {
	normalized := normalizeShell(shell)
	switch normalized {
	case "bash":
		return "bash -s", nil
	case "ash":
		return "ash -s", nil
	case "zsh":
		return "zsh -s", nil
	case "sh", "":
		return "sh -s", nil
	case "powershell":
		return powerShellEncodedCommand(script), nil
	case "cmd":
		return "", fmt.Errorf("cmd does not support stdin script injection — use PowerShell or specify --shell powershell")
	default:
		return normalized + " -s", nil
	}
}

func RunBufferedWithShell(ctx context.Context, client *sshclient.Client, command, shell string) (*Result, error) {
	switch normalizeShell(shell) {
	case "powershell":
		return RunBuffered(ctx, client, powerShellEncodedCommand([]byte(command)))
	case "cmd":
		return RunBuffered(ctx, client, "cmd /C "+command)
	default:
		return RunBuffered(ctx, client, command)
	}
}

func normalizeShell(shell string) string {
	switch strings.ToLower(strings.TrimSpace(shell)) {
	case "powershell.exe", "pwsh", "pwsh.exe":
		return "powershell"
	case "cmd.exe":
		return "cmd"
	default:
		return strings.ToLower(strings.TrimSpace(shell))
	}
}

func RunScript(ctx context.Context, client *sshclient.Client, script []byte, shell string, stdout, stderr io.Writer) (int, error) {
	cmd, err := InterpreterCmd(shell, script)
	if err != nil {
		return -1, err
	}

	session, err := client.NewSession()
	if err != nil {
		return -1, fmt.Errorf("create session: %w", err)
	}
	defer session.Close()

	if normalizeShell(shell) == "powershell" {
		session.Stdout = stdout
		session.Stderr = stderr
		return runStartedSession(ctx, session, cmd)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		return -1, fmt.Errorf("stdin pipe: %w", err)
	}

	session.Stdout = stdout
	session.Stderr = stderr

	if err := session.Start(cmd); err != nil {
		return -1, fmt.Errorf("start interpreter %q: %w", cmd, err)
	}

	done := make(chan error, 1)
	go func() {
		_, writeErr := stdin.Write(script)
		stdin.Close()
		if writeErr != nil {
			done <- writeErr
			return
		}
		done <- session.Wait()
	}()

	select {
	case <-ctx.Done():
		session.Signal(ssh.SIGKILL)
		session.Close()
		return -1, fmt.Errorf("script execution cancelled: %w", ctx.Err())
	case err := <-done:
		return exitCode(err), nil
	}
}

func RunScriptBuffered(ctx context.Context, client *sshclient.Client, script []byte, shell string, options ...ScriptOption) (*Result, error) {
	if normalizeShell(shell) == "powershell" {
		opts := scriptOptions{}
		for _, option := range options {
			option(&opts)
		}
		if opts.ops == nil {
			opts.ops = &sshPowerShellScriptOps{client: client, profile: opts.profile, verbose: opts.verbose}
		}
		return runPowerShellScriptBuffered(ctx, script, opts)
	}

	cmd, err := InterpreterCmd(shell, script)
	if err != nil {
		return nil, err
	}

	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	defer session.Close()

	stdin, err := session.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &limitedWriter{w: &stdoutBuf, remaining: MaxBufferedBytes}
	session.Stderr = &limitedWriter{w: &stderrBuf, remaining: MaxBufferedBytes}

	if err := session.Start(cmd); err != nil {
		return nil, fmt.Errorf("start interpreter %q: %w", cmd, err)
	}

	done := make(chan error, 1)
	go func() {
		_, writeErr := stdin.Write(script)
		stdin.Close()
		if writeErr != nil {
			done <- writeErr
			return
		}
		done <- session.Wait()
	}()

	select {
	case <-ctx.Done():
		session.Signal(ssh.SIGKILL)
		session.Close()
		return nil, fmt.Errorf("script execution cancelled: %w", ctx.Err())
	case err := <-done:
		return &Result{
			ExitCode: exitCode(err),
			Stdout:   stdoutBuf.String(),
			Stderr:   stderrBuf.String(),
		}, nil
	}
}

func runPowerShellScriptBuffered(ctx context.Context, script []byte, opts scriptOptions) (*Result, error) {
	if len(script) <= powerShellInlineLimit {
		// CLIXML stderr decoding happens in the CLI layer after codepage
		// transcoding — the raw bytes here may be GBK etc., which the UTF-8
		// only XML parser would reject.
		return opts.ops.Run(ctx, powerShellEncodedCommand(script))
	}
	if opts.verbose != nil {
		opts.verbose("script exceeds inline limit, using upload-run")
	}

	remotePath, err := powerShellTempPath(opts.profile)
	if err != nil {
		return nil, err
	}
	localPath, err := writePowerShellTempFile(script)
	if err != nil {
		return nil, err
	}
	defer os.Remove(localPath)

	if err := opts.ops.Upload(ctx, localPath, remotePath); err != nil {
		return nil, err
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if err := opts.ops.Remove(cleanupCtx, remotePath); err != nil && opts.verbose != nil {
			opts.verbose("remote script cleanup failed: " + err.Error())
		}
	}()

	return opts.ops.Run(ctx, powerShellPrefix+" -File "+powerShellFileArg(remotePath))
}

func powerShellEncodedCommand(script []byte) string {
	return powershell.EncodedCommand(script)
}

func powerShellTempPath(profile *remote.Profile) (string, error) {
	if profile == nil {
		return "", fmt.Errorf("remote profile required for PowerShell upload-run fallback")
	}
	tempDir := profile.TempDir
	if tempDir == "" && profile.HomeDir != "" {
		tempDir = strings.TrimRight(profile.HomeDir, `/\`) + `/AppData/Local/Temp`
	}
	if tempDir == "" {
		return "", fmt.Errorf("remote temp directory unavailable for PowerShell upload-run fallback")
	}

	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate remote script name: %w", err)
	}
	tempDir = strings.ReplaceAll(strings.TrimRight(tempDir, `/\`), `\`, "/")
	return tempDir + "/sshq-script-" + hex.EncodeToString(random) + ".ps1", nil
}

func writePowerShellTempFile(script []byte) (string, error) {
	file, err := os.CreateTemp("", "sshq-script-*.ps1")
	if err != nil {
		return "", fmt.Errorf("create local PowerShell temp file: %w", err)
	}
	path := file.Name()
	cleanup := func() {
		file.Close()
		os.Remove(path)
	}

	if !bytes.HasPrefix(script, []byte{0xef, 0xbb, 0xbf}) {
		if _, err := file.Write([]byte{0xef, 0xbb, 0xbf}); err != nil {
			cleanup()
			return "", fmt.Errorf("write PowerShell BOM: %w", err)
		}
	}
	if _, err := file.Write(script); err != nil {
		cleanup()
		return "", fmt.Errorf("write PowerShell script: %w", err)
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("close local PowerShell temp file: %w", err)
	}
	return path, nil
}

func powerShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func powerShellFileArg(value string) string {
	return `"` + value + `"`
}

func runStartedSession(ctx context.Context, session *ssh.Session, command string) (int, error) {
	if err := session.Start(command); err != nil {
		return -1, fmt.Errorf("start interpreter %q: %w", command, err)
	}
	done := make(chan error, 1)
	go func() { done <- session.Wait() }()
	select {
	case <-ctx.Done():
		session.Signal(ssh.SIGKILL)
		session.Close()
		return -1, fmt.Errorf("script execution cancelled: %w", ctx.Err())
	case err := <-done:
		return exitCode(err), nil
	}
}
