package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/shayuc137/sshq/internal/ipc"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/transfer"
)

func TestTransferPayloadCarriesMkdirs(t *testing.T) {
	parsed, err := transfer.ParseArgs("./local file.txt", "win:C:/Work Files/out.txt")
	if err != nil {
		t.Fatal(err)
	}
	payload := transferPayload(parsed, false, true, false, 12)
	if !payload.Mkdirs || payload.Timeout != 12 || payload.Direction != "upload" || payload.RemotePath != "C:/Work Files/out.txt" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestContextWithTransferTimeout(t *testing.T) {
	unlimited, cancelUnlimited := contextWithTransferTimeout(0)
	defer cancelUnlimited()
	if _, ok := unlimited.Deadline(); ok {
		t.Fatal("Timeout: 0 created a deadline")
	}

	limited, cancelLimited := contextWithTransferTimeout(1)
	defer cancelLimited()
	deadline, ok := limited.Deadline()
	if !ok {
		t.Fatal("non-zero timeout did not create a deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 || remaining > time.Second {
		t.Fatalf("deadline remaining = %s, want (0, 1s]", remaining)
	}
}

func TestCpErrorToOutput(t *testing.T) {
	upload, err := transfer.ParseArgs("local.bin", "web:/tmp/local.bin")
	if err != nil {
		t.Fatal(err)
	}
	relay, err := transfer.ParseArgs("web:/tmp/a", "backup:/tmp/a")
	if err != nil {
		t.Fatal(err)
	}

	deadlineCtx, cancelDeadline := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelDeadline()
	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name   string
		err    error
		ctx    context.Context
		parsed transfer.ParsedArgs
		code   string
		hint   string
		action string
	}{
		{
			name:   "deadline",
			err:    context.DeadlineExceeded,
			ctx:    deadlineCtx,
			parsed: relay,
			code:   output.CodeTimeout,
			hint:   "relay deadline exceeded; partial data may have been transferred; temporary files cleaned up",
			action: "increase --timeout and retry",
		},
		{
			name:   "cancelled",
			err:    context.Canceled,
			ctx:    cancelledCtx,
			parsed: upload,
			code:   output.CodeTransferFailed,
			hint:   "transfer cancelled; temporary file cleaned up",
			action: "re-run the command to retry",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cpErrorToOutput(tt.ctx, tt.err, tt.parsed, false)
			if got.Code != tt.code || got.Hint != tt.hint || got.Action != tt.action {
				t.Fatalf("error = %+v, want code=%q hint=%q action=%q", got, tt.code, tt.hint, tt.action)
			}
		})
	}
}

func TestCpErrorToOutputMatchesDaemonFrame(t *testing.T) {
	parsed, err := transfer.ParseArgs("local.bin", "web:/tmp/local.bin")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	direct := cpErrorToOutput(ctx, context.DeadlineExceeded, parsed, false)

	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		serverConn.Close()
		clientConn.Close()
	})
	go func() {
		_ = ipc.SendError(serverConn, direct.Code, direct.Hint, direct.Action)
	}()
	daemonErr := recvTransferFrames(output.New(io.Discard, io.Discard), clientConn, time.Time{})
	var daemon *output.CmdError
	if !errors.As(daemonErr, &daemon) {
		t.Fatalf("daemon error = %T %v", daemonErr, daemonErr)
	}
	if daemon.Code != direct.Code || daemon.Hint != direct.Hint || daemon.Action != direct.Action {
		t.Fatalf("daemon error = %+v, direct error = %+v", daemon, direct)
	}
}

func TestCpMkdirsActionIsExecutable(t *testing.T) {
	parsed, err := transfer.ParseArgs("./local file.txt", "win:C:/Work Files/out.txt")
	if err != nil {
		t.Fatal(err)
	}
	action := cpMkdirsAction(parsed, false)
	for _, want := range []string{"sshq cp --mkdirs", "'./local file.txt'", "'win:C:/Work Files/out.txt'"} {
		if !strings.Contains(action, want) {
			t.Fatalf("action %q missing %q", action, want)
		}
	}
}

func TestCpCommandRegistersMkdirs(t *testing.T) {
	flag := newCpCommand().Flags().Lookup("mkdirs")
	if flag == nil || flag.DefValue != "false" {
		t.Fatalf("mkdirs flag = %+v", flag)
	}
}

// net.Pipe honors deadlines exactly like the unix socket the daemon uses, so a
// fake connection is a sound stand-in here. That is not true of every transport
// — the SSH channel a transfer runs over has no deadline at all, which is why
// the daemon side cannot break out of a wedged copy.
func TestRecvTransferFramesGivesUpWhenDaemonGoesQuiet(t *testing.T) {
	old := frameStallTimeout
	frameStallTimeout = 40 * time.Millisecond
	t.Cleanup(func() { frameStallTimeout = old })

	_, clientConn := net.Pipe()
	t.Cleanup(func() { clientConn.Close() })

	start := time.Now()
	err := recvTransferFrames(output.New(io.Discard, io.Discard), clientConn, time.Time{})
	elapsed := time.Since(start)

	var cmdErr *output.CmdError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("error = %T %v", err, err)
	}
	if cmdErr.Code != output.CodeResultIndeterminate {
		t.Fatalf("code = %q, want %q", cmdErr.Code, output.CodeResultIndeterminate)
	}
	if strings.Contains(cmdErr.Hint, "cleaned up") {
		t.Fatalf("hint claims cleanup the client cannot verify: %q", cmdErr.Hint)
	}
	if elapsed < frameStallTimeout || elapsed > 2*time.Second {
		t.Fatalf("returned after %s, want roughly %s", elapsed, frameStallTimeout)
	}
}

func TestRecvTransferFramesKeepsWaitingWhileFramesArrive(t *testing.T) {
	old := frameStallTimeout
	frameStallTimeout = 60 * time.Millisecond
	t.Cleanup(func() { frameStallTimeout = old })

	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		serverConn.Close()
		clientConn.Close()
	})

	// Five progress frames spaced under the stall window outlive it in total:
	// the deadline tracks silence, not elapsed time.
	go func() {
		for i := 0; i < 5; i++ {
			payload, _ := json.Marshal(transfer.ProgressInfo{File: "big.bin", Percent: i * 20, Total: 100})
			_ = ipc.Send(serverConn, ipc.Frame{Type: "progress", Payload: payload})
			time.Sleep(20 * time.Millisecond)
		}
		result, _ := json.Marshal(transfer.Result{Direction: "upload", Engine: "sftp"})
		_ = ipc.Send(serverConn, ipc.Frame{Type: "result", Payload: result})
	}()

	if err := recvTransferFrames(output.New(io.Discard, io.Discard), clientConn, time.Time{}); err != nil {
		t.Fatalf("live transfer was interrupted: %v", err)
	}
}

func TestRecvTransferFramesPrefersDaemonTimeoutFrame(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		serverConn.Close()
		clientConn.Close()
	})

	// The daemon reports within the grace window, so its precise message must
	// win over this side's indeterminate fallback.
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = ipc.SendError(serverConn, output.CodeTimeout, "transfer deadline exceeded", "increase --timeout and retry")
	}()

	err := recvTransferFrames(output.New(io.Discard, io.Discard), clientConn, time.Now().Add(time.Second))
	var cmdErr *output.CmdError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("error = %T %v", err, err)
	}
	if cmdErr.Code != output.CodeTimeout {
		t.Fatalf("code = %q, want %q — the client fallback preempted the daemon", cmdErr.Code, output.CodeTimeout)
	}
}
