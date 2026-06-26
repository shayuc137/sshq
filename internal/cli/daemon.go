package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/shayuc137/sshq/internal/config"
	"github.com/shayuc137/sshq/internal/ipc"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/pool"
	"github.com/shayuc137/sshq/internal/sshclient"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
)

const defaultIdleTimeout = 30 * time.Minute
const reapInterval = 60 * time.Second

func newDaemonCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the connection pool daemon",
	}
	cmd.AddCommand(
		newDaemonStartCommand(),
		newDaemonStopCommand(),
		newDaemonStatusCommand(),
	)
	return cmd
}

func newDaemonStartCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the connection pool daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if ipc.IsRunning() {
				return output.Errorf("daemon already running", "use 'sshq daemon status' to check")
			}

			store := configFrom(cmd.Context())
			if store == nil {
				return output.Errorf("no SSH config loaded", "check ~/.ssh/config exists")
			}

			w := writerFrom(cmd.Context())
			return runDaemon(w, store)
		},
	}
}

func newDaemonStopCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, err := ipc.Connect()
			if err != nil {
				return output.Errorf("daemon not running", "")
			}
			defer conn.Close()

			req := ipc.Request{Action: "shutdown", ProtocolVersion: ipc.ProtocolVersion}
			if err := ipc.Send(conn, req); err != nil {
				return output.Errorf("send shutdown: "+err.Error(), "")
			}

			w := writerFrom(cmd.Context())
			w.Success("daemon stopped")
			return nil
		},
	}
}

func newDaemonStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show daemon status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, err := ipc.Connect()
			if err != nil {
				w := writerFrom(cmd.Context())
				w.Value("daemon not running")
				return nil
			}
			defer conn.Close()

			req := ipc.Request{Action: "status", ProtocolVersion: ipc.ProtocolVersion}
			if err := ipc.Send(conn, req); err != nil {
				return output.Errorf("send status: "+err.Error(), "")
			}

			msg, err := ipc.Recv(conn)
			if err != nil {
				return output.Errorf("recv status: "+err.Error(), "")
			}

			w := writerFrom(cmd.Context())
			if w.IsJSONMode() {
				var resp ipc.StatusResponse
				json.Unmarshal(msg, &resp)
				w.JSONOut(resp)
				return nil
			}

			var resp ipc.StatusResponse
			json.Unmarshal(msg, &resp)
			w.Value(renderDaemonStatus(resp))
			return nil
		},
	}
}

func renderDaemonStatus(resp ipc.StatusResponse) string {
	s := fmt.Sprintf("daemon running uptime=%ds connections=%d\n", resp.Uptime, len(resp.Connections))
	for _, c := range resp.Connections {
		idle := time.Since(time.Unix(c.IdleSince, 0)).Truncate(time.Second)
		s += fmt.Sprintf("  %s %s:%s idle=%s\n", c.Alias, c.Host, c.Port, idle)
	}
	return s
}

func runDaemon(w *output.Writer, store *config.Store) error {
	sockPath := ipc.SocketPath()
	os.Remove(sockPath)

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return output.Errorf("listen: "+err.Error(), "")
	}
	defer ln.Close()
	os.Chmod(sockPath, 0600)

	if err := writePID(); err != nil {
		return output.Errorf("write PID: "+err.Error(), "")
	}
	defer removePID()
	defer os.Remove(sockPath)

	p := pool.New(defaultIdleTimeout)
	startTime := time.Now()
	lastActivity := time.Now()
	var mu sync.Mutex

	w.Success(fmt.Sprintf("daemon started on %s", sockPath))

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	stopCh := make(chan struct{})
	var stopped bool

	go func() {
		ticker := time.NewTicker(reapInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				p.Reap()
				mu.Lock()
				idle := time.Since(lastActivity)
				mu.Unlock()
				if idle > defaultIdleTimeout {
					w.Info("idle timeout, shutting down")
					close(stopCh)
					return
				}
			case <-stopCh:
				return
			}
		}
	}()

	go func() {
		select {
		case <-sigCh:
			if !stopped {
				stopped = true
				close(stopCh)
			}
		case <-stopCh:
		}
	}()

	go func() {
		<-stopCh
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-stopCh:
				p.CloseAll()
				w.Info("daemon stopped")
				return nil
			default:
				continue
			}
		}

		mu.Lock()
		lastActivity = time.Now()
		mu.Unlock()

		go handleConn(conn, store, p, startTime, stopCh, &stopped)
	}
}

func handleConn(conn net.Conn, store *config.Store, p *pool.Pool, startTime time.Time, stopCh chan struct{}, stopped *bool) {
	defer conn.Close()

	msg, err := ipc.Recv(conn)
	if err != nil {
		return
	}

	var req ipc.Request
	if err := json.Unmarshal(msg, &req); err != nil {
		ipc.Send(conn, ipc.Frame{Type: "error", Hint: "invalid request"})
		return
	}

	if req.ProtocolVersion != ipc.ProtocolVersion {
		ipc.Send(conn, ipc.Frame{Type: "error", Hint: fmt.Sprintf("protocol version mismatch: got %d, want %d", req.ProtocolVersion, ipc.ProtocolVersion)})
		return
	}

	switch req.Action {
	case "exec":
		handleExec(conn, store, p, req)
	case "status":
		handleStatus(conn, p, startTime)
	case "shutdown":
		if !*stopped {
			*stopped = true
			close(stopCh)
		}
	}
}

func handleExec(conn net.Conn, store *config.Store, p *pool.Pool, req ipc.Request) {
	host, err := store.Get(req.Alias)
	if err != nil {
		ipc.Send(conn, ipc.Frame{Type: "error", Hint: err.Error(), Action: "run 'sshq ls' to see available hosts"})
		return
	}

	cfg := sshclient.ConnConfig{
		Host:         host.HostName,
		Port:         host.Port,
		User:         host.User,
		IdentityFile: host.IdentityFile,
		Timeout:      time.Duration(req.Timeout) * time.Second,
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	client, err := p.Get(context.Background(), req.Alias, cfg)
	if err != nil {
		ipc.Send(conn, ipc.Frame{Type: "error", Hint: err.Error(), Action: "check connectivity and credentials"})
		return
	}

	session, err := client.NewSession()
	if err != nil {
		ipc.Send(conn, ipc.Frame{Type: "error", Hint: "create session: " + err.Error()})
		return
	}
	defer session.Close()

	stdoutPipe, _ := session.StdoutPipe()
	stderrPipe, _ := session.StderrPipe()

	if err := session.Start(req.Command); err != nil {
		ipc.Send(conn, ipc.Frame{Type: "error", Hint: "start command: " + err.Error()})
		return
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		streamPipe(conn, stdoutPipe, "stdout")
	}()

	go func() {
		defer wg.Done()
		streamPipe(conn, stderrPipe, "stderr")
	}()

	wg.Wait()

	exitCode := 0
	if err := session.Wait(); err != nil {
		if exitErr, ok := err.(*ssh.ExitError); ok {
			exitCode = exitErr.ExitStatus()
		} else {
			ipc.Send(conn, ipc.Frame{Type: "error", Hint: "wait: " + err.Error()})
			return
		}
	}

	ipc.Send(conn, ipc.Frame{Type: "exit", Code: exitCode})
}

func streamPipe(conn net.Conn, pipe io.Reader, frameType string) {
	buf := make([]byte, 32*1024)
	for {
		n, err := pipe.Read(buf)
		if n > 0 {
			ipc.Send(conn, ipc.Frame{Type: frameType, Data: string(buf[:n])})
		}
		if err != nil {
			return
		}
	}
}

func handleStatus(conn net.Conn, p *pool.Pool, startTime time.Time) {
	stats := p.Stats()
	conns := make([]ipc.ConnInfo, len(stats))
	for i, s := range stats {
		conns[i] = ipc.ConnInfo{
			Key:       s.Key,
			Alias:     s.Alias,
			Host:      s.Host,
			Port:      s.Port,
			IdleSince: s.IdleSince.Unix(),
		}
	}
	resp := ipc.StatusResponse{
		Running:     true,
		Uptime:      int64(time.Since(startTime).Seconds()),
		Connections: conns,
	}
	ipc.Send(conn, resp)
}

func writePID() error {
	return os.WriteFile(ipc.PIDPath(), []byte(strconv.Itoa(os.Getpid())), 0600)
}

func removePID() {
	os.Remove(ipc.PIDPath())
}
