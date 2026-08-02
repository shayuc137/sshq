package cli

import (
	"context"
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
	daemonErr := recvTransferFrames(output.New(io.Discard, io.Discard), clientConn)
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
