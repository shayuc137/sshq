package cli

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/shayuc137/sshq/internal/ipc"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/remote"
)

const daemonVerboseFrame = "verbose"

func recvVerboseFrame(w *output.Writer, frame ipc.Frame) {
	msg := strings.TrimRight(frame.Data, "\n")
	if msg == "" {
		return
	}
	w.Verbose(msg)
}

func sendDaemonVerbose(conn net.Conn, enabled bool, format string, args ...any) {
	if !enabled {
		return
	}
	ipc.Send(conn, ipc.Frame{Type: daemonVerboseFrame, Data: fmt.Sprintf(format, args...)})
}

func sendDaemonVerboseLocked(conn net.Conn, mu *sync.Mutex, enabled bool, format string, args ...any) {
	if !enabled {
		return
	}
	mu.Lock()
	sendDaemonVerbose(conn, true, format, args...)
	mu.Unlock()
}

func verboseDuration(d time.Duration) string {
	return d.Truncate(time.Millisecond).String()
}

func verboseProfile(p *remote.Profile) string {
	if p == nil {
		return "shell detection: profile=unknown"
	}
	parts := []string{
		"shell detection:",
		"os=" + string(p.OS),
		"shell=" + string(p.Shell),
	}
	if p.Encoding != "" {
		parts = append(parts, "encoding="+p.Encoding)
	}
	return strings.Join(parts, " ")
}
