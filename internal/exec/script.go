package exec

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/shayuc137/sshq/internal/sshclient"
	"golang.org/x/crypto/ssh"
)

func InterpreterCmd(shell string) (string, error) {
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
		return "powershell -NoProfile -NonInteractive -Command -", nil
	case "cmd":
		return "", fmt.Errorf("cmd does not support stdin script injection — use PowerShell or specify --shell powershell")
	default:
		return normalized + " -s", nil
	}
}

func RunBufferedWithShell(ctx context.Context, client *sshclient.Client, command, shell string) (*Result, error) {
	switch normalizeShell(shell) {
	case "powershell":
		return RunScriptBuffered(ctx, client, []byte(command), "powershell")
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
	cmd, err := InterpreterCmd(shell)
	if err != nil {
		return -1, err
	}

	session, err := client.NewSession()
	if err != nil {
		return -1, fmt.Errorf("create session: %w", err)
	}
	defer session.Close()

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

func RunScriptBuffered(ctx context.Context, client *sshclient.Client, script []byte, shell string) (*Result, error) {
	cmd, err := InterpreterCmd(shell)
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
