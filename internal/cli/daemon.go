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
	"github.com/shayuc137/sshq/internal/remote"
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
			return sendSimpleAction("shutdown", "daemon stopped", cmd)
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

			env, _ := ipc.MakeEnvelope("status", nil)
			if err := ipc.Send(conn, env); err != nil {
				return output.Errorf("send status: "+err.Error(), "")
			}

			msg, err := ipc.Recv(conn)
			if err != nil {
				return output.Errorf("recv status: "+err.Error(), "")
			}

			w := writerFrom(cmd.Context())
			var resp ipc.StatusResponse
			json.Unmarshal(msg, &resp)

			if w.IsJSONMode() {
				w.JSONOut(resp)
			} else {
				w.Value(renderDaemonStatus(resp))
			}
			return nil
		},
	}
}

func sendSimpleAction(action, successMsg string, cmd *cobra.Command) error {
	conn, err := ipc.Connect()
	if err != nil {
		return output.Errorf("daemon not running", "")
	}
	defer conn.Close()

	env, _ := ipc.MakeEnvelope(action, nil)
	if err := ipc.Send(conn, env); err != nil {
		return output.Errorf("send "+action+": "+err.Error(), "")
	}

	w := writerFrom(cmd.Context())
	w.Success(successMsg)
	return nil
}

func renderDaemonStatus(resp ipc.StatusResponse) string {
	s := fmt.Sprintf("daemon running uptime=%ds connections=%d\n", resp.Uptime, len(resp.Connections))
	for _, c := range resp.Connections {
		idle := time.Since(time.Unix(c.IdleSince, 0)).Truncate(time.Second)
		s += fmt.Sprintf("  %s %s:%s idle=%s\n", c.Alias, c.Host, c.Port, idle)
	}
	return s
}

// --- daemon server ---

type daemonContext struct {
	store      *config.Store
	pool       *pool.Pool
	cache      *remote.Cache
	startTime  time.Time
	stopCh     chan struct{}
	stopped    *bool
	stoppedMu  sync.Mutex
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

	cache, _ := remote.NewCache(remote.DefaultTTL)

	stopped := false
	dc := &daemonContext{
		store:     store,
		pool:      pool.New(defaultIdleTimeout),
		cache:     cache,
		startTime: time.Now(),
		stopCh:    make(chan struct{}),
		stopped:   &stopped,
	}

	lastActivity := time.Now()
	var mu sync.Mutex

	w.Success(fmt.Sprintf("daemon started on %s (protocol v%d)", sockPath, ipc.ProtocolVersion))

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		ticker := time.NewTicker(reapInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				dc.pool.Reap()
				mu.Lock()
				idle := time.Since(lastActivity)
				mu.Unlock()
				if idle > defaultIdleTimeout {
					w.Info("idle timeout, shutting down")
					dc.shutdown()
					return
				}
			case <-dc.stopCh:
				return
			}
		}
	}()

	go func() {
		select {
		case <-sigCh:
			dc.shutdown()
		case <-dc.stopCh:
		}
	}()

	go func() {
		<-dc.stopCh
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-dc.stopCh:
				dc.pool.CloseAll()
				w.Info("daemon stopped")
				return nil
			default:
				continue
			}
		}

		mu.Lock()
		lastActivity = time.Now()
		mu.Unlock()

		go dc.handleConn(conn)
	}
}

func (dc *daemonContext) shutdown() {
	dc.stoppedMu.Lock()
	defer dc.stoppedMu.Unlock()
	if !*dc.stopped {
		*dc.stopped = true
		close(dc.stopCh)
	}
}

func (dc *daemonContext) handleConn(conn net.Conn) {
	defer conn.Close()

	msg, err := ipc.Recv(conn)
	if err != nil {
		return
	}

	if ipc.DetectV1(msg) {
		ipc.SendError(conn,
			"protocol v1 is deprecated, upgrade sshq CLI",
			"go install github.com/shayuc137/sshq/cmd/sshq@latest",
		)
		return
	}

	env, err := ipc.ParseEnvelope(msg)
	if err != nil {
		ipc.SendError(conn, "invalid request: "+err.Error(), "")
		return
	}

	if env.Version != ipc.ProtocolVersion {
		ipc.SendError(conn,
			fmt.Sprintf("protocol version mismatch: got %d, want %d", env.Version, ipc.ProtocolVersion),
			"go install github.com/shayuc137/sshq/cmd/sshq@latest",
		)
		return
	}

	dc.route(conn, env)
}

func (dc *daemonContext) route(conn net.Conn, env ipc.Envelope) {
	switch env.Action {
	case "exec":
		dc.handleExec(conn, env.Payload)
	case "script":
		dc.handleScript(conn, env.Payload)
	case "transfer":
		dc.handleTransfer(conn, env.Payload)
	case "relay":
		dc.handleRelay(conn, env.Payload)
	case "profile":
		dc.handleProfile(conn, env.Payload)
	case "cluster-exec":
		dc.handleClusterExec(conn, env.Payload)
	case "status":
		dc.handleStatus(conn)
	case "shutdown":
		dc.shutdown()
	default:
		ipc.SendError(conn, "unknown action: "+env.Action, "")
	}
}

func (dc *daemonContext) handleExec(conn net.Conn, raw json.RawMessage) {
	var payload ipc.ExecPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		ipc.SendError(conn, "invalid exec payload: "+err.Error(), "")
		return
	}

	host, err := dc.store.Get(payload.Alias)
	if err != nil {
		ipc.SendError(conn, err.Error(), "run 'sshq ls' to see available hosts")
		return
	}

	cfg := hostToConnConfigWithStore(host, dc.store)
	cfg.Timeout = time.Duration(payload.Timeout) * time.Second
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	client, err := dc.pool.Get(context.Background(), payload.Alias, cfg)
	if err != nil {
		ce := connErrorToOutput(err, payload.Alias)
		ipc.SendError(conn, ce.Hint, ce.Action)
		return
	}

	session, err := client.NewSession()
	if err != nil {
		ipc.SendError(conn, "create session: "+err.Error(), "")
		return
	}
	defer session.Close()

	stdoutPipe, _ := session.StdoutPipe()
	stderrPipe, _ := session.StderrPipe()

	if err := session.Start(payload.Command); err != nil {
		ipc.SendError(conn, "start command: "+err.Error(), "")
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
			ipc.SendError(conn, "wait: "+err.Error(), "")
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

func (dc *daemonContext) handleStatus(conn net.Conn) {
	stats := dc.pool.Stats()
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
		Uptime:      int64(time.Since(dc.startTime).Seconds()),
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
