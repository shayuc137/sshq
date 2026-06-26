package exec

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"golang.org/x/crypto/ssh"
)

const MaxBufferedBytes = 10 * 1024 * 1024 // 10MB

type ExitError struct {
	Code int
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("exit %d", e.Code)
}

func Run(ctx context.Context, client *ssh.Client, command string, stdout, stderr io.Writer) (int, error) {
	session, err := client.NewSession()
	if err != nil {
		return -1, fmt.Errorf("create session: %w", err)
	}
	defer session.Close()

	session.Stdout = stdout
	session.Stderr = stderr

	done := make(chan error, 1)
	go func() {
		done <- session.Run(command)
	}()

	select {
	case <-ctx.Done():
		session.Signal(ssh.SIGKILL)
		session.Close()
		return -1, fmt.Errorf("execution cancelled: %w", ctx.Err())
	case err := <-done:
		return exitCode(err), nil
	}
}

type Result struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

func RunBuffered(ctx context.Context, client *ssh.Client, command string) (*Result, error) {
	session, err := client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	defer session.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &limitedWriter{w: &stdoutBuf, remaining: MaxBufferedBytes}
	session.Stderr = &limitedWriter{w: &stderrBuf, remaining: MaxBufferedBytes}

	done := make(chan error, 1)
	go func() {
		done <- session.Run(command)
	}()

	select {
	case <-ctx.Done():
		session.Signal(ssh.SIGKILL)
		session.Close()
		return nil, fmt.Errorf("execution cancelled: %w", ctx.Err())
	case err := <-done:
		return &Result{
			ExitCode: exitCode(err),
			Stdout:   stdoutBuf.String(),
			Stderr:   stderrBuf.String(),
		}, nil
	}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*ssh.ExitError); ok {
		return exitErr.ExitStatus()
	}
	return 1
}

type limitedWriter struct {
	w         io.Writer
	remaining int64
}

func (lw *limitedWriter) Write(p []byte) (int, error) {
	if lw.remaining <= 0 {
		return 0, fmt.Errorf("output exceeded %d bytes limit — use non-JSON mode for large output", MaxBufferedBytes)
	}
	if int64(len(p)) > lw.remaining {
		p = p[:lw.remaining]
	}
	n, err := lw.w.Write(p)
	lw.remaining -= int64(n)
	return n, err
}
