package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/shayuc137/sshq/internal/appconfig"
	"github.com/shayuc137/sshq/internal/audit"
	"github.com/shayuc137/sshq/internal/config"
	"github.com/shayuc137/sshq/internal/credential"
	"github.com/shayuc137/sshq/internal/exec"
	"github.com/shayuc137/sshq/internal/ipc"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/policy"
	"github.com/shayuc137/sshq/internal/pool"
	"github.com/shayuc137/sshq/internal/remote"
	"github.com/shayuc137/sshq/internal/tunnel"
	"github.com/spf13/cobra"
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
			return runDaemon(w, store, credentialStoreFrom(cmd.Context()))
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
			w := writerFrom(cmd.Context())
			conn, err := ipc.Connect()
			if err != nil {
				w.Render(ipc.StatusResponse{Running: false})
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

			var resp ipc.StatusResponse
			json.Unmarshal(msg, &resp)
			w.Render(resp)
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

// --- daemon server ---

type daemonContext struct {
	store     *config.Store
	creds     *credential.Store
	appConfig *appconfig.Config
	appCfgErr error
	checker   *policy.Checker
	grants    *policy.GrantManager
	auditLog  *audit.Logger
	pool      *pool.Pool
	cache     *remote.Cache
	tunnels   *tunnel.Registry
	startTime time.Time
	stopCh    chan struct{}
	stopped   *bool
	stoppedMu sync.Mutex
}

func runDaemon(w *output.Writer, store *config.Store, creds *credential.Store) error {
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

	cache, err := remote.NewCache(remote.DefaultTTL, remote.WithCacheInfo(func(msg string) {
		w.Info("warning: " + msg)
	}))
	if err != nil {
		w.Info("warning: profile cache unavailable: " + err.Error())
	}

	if creds == nil {
		var err error
		creds, err = credential.Open()
		if err != nil {
			w.Info("warning: credential store unavailable: " + err.Error())
		}
	}

	appCfg, appCfgErr := appconfig.Load()
	if appCfgErr != nil {
		w.Info("warning: app config unavailable: " + appCfgErr.Error())
	}
	var auditLog *audit.Logger
	if audit.Enabled(appCfg) {
		auditLog, err = audit.NewLogger(appCfg.Audit)
		if err != nil {
			w.Info("warning: audit log unavailable: " + err.Error())
		}
	}
	defer auditLog.Close()
	grants := policy.NewGrantManager()

	stopped := false
	dc := &daemonContext{
		store:     store,
		creds:     creds,
		appConfig: appCfg,
		appCfgErr: appCfgErr,
		checker:   policy.NewChecker(appCfg, grants),
		grants:    grants,
		auditLog:  auditLog,
		pool:      pool.New(defaultIdleTimeout),
		cache:     cache,
		tunnels:   tunnel.NewRegistry(),
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
				dc.grants.Purge()
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
				dc.tunnels.StopAll()
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
			"restart daemon so CLI and daemon use the same sshq version",
		)
		return
	}

	dc.route(conn, env)
}

func (dc *daemonContext) route(conn net.Conn, env ipc.Envelope) {
	if err := dc.reloadPolicy(); err != nil && isPolicySensitiveAction(env.Action) {
		ipc.SendError(conn, "app config invalid: "+err.Error(), "fix config.toml, then retry")
		return
	}

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
	case "tunnel-start":
		dc.handleTunnelStart(conn, env.Payload)
	case "tunnel-stop":
		dc.handleTunnelStop(conn, env.Payload)
	case "tunnel-list":
		dc.handleTunnelList(conn)
	case "policy-grant":
		dc.handlePolicyGrant(conn, env.Payload)
	case "policy-revoke":
		dc.handlePolicyRevoke(conn, env.Payload)
	case "policy-list":
		dc.handlePolicyList(conn, env.Payload)
	case "status":
		dc.handleStatus(conn)
	case "shutdown":
		dc.shutdown()
	default:
		ipc.SendError(conn, "unknown action: "+env.Action, "")
	}
}

func (dc *daemonContext) reloadPolicy() error {
	if dc.appConfig == nil {
		cfg, err := appconfig.Load()
		if err != nil {
			dc.appCfgErr = err
			return err
		}
		dc.appConfig = cfg
		dc.appCfgErr = nil
		dc.checker.Reload(cfg)
		return nil
	}

	changed, err := dc.appConfig.ReloadIfChanged()
	if err != nil {
		dc.appCfgErr = err
		return err
	}
	if changed {
		dc.checker.Reload(dc.appConfig)
	}
	dc.appCfgErr = nil
	return nil
}

func isPolicySensitiveAction(action string) bool {
	switch action {
	case "exec", "script", "transfer", "relay", "cluster-exec":
		return true
	default:
		return false
	}
}

func (dc *daemonContext) handleExec(conn net.Conn, raw json.RawMessage) {
	var payload ipc.ExecPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		ipc.SendError(conn, "invalid exec payload: "+err.Error(), "")
		return
	}
	if !dc.checkDaemonCommand(conn, payload.Alias, payload.Command) {
		return
	}

	host, err := dc.store.Get(payload.Alias)
	if err != nil {
		entry := audit.ExecErrorEntry(payload.Alias, payload.Command, audit.ResultError, 0, audit.SourceDaemon, err)
		if !dc.sendAudit(conn, entry) {
			return
		}
		ipc.SendError(conn, err.Error(), "run 'sshq ls' to see available hosts")
		return
	}

	cfg := hostToConnConfigWithCredentials(host, dc.store, dc.creds)
	cfg.Timeout = time.Duration(payload.Timeout) * time.Second
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	connectStart := time.Now()
	client, reused, err := dc.pool.GetWithStatus(ctx, payload.Alias, cfg)
	if err != nil {
		entry := audit.ExecErrorEntry(payload.Alias, payload.Command, audit.ResultError, 0, audit.SourceDaemon, err)
		if !dc.sendAudit(conn, entry) {
			return
		}
		ce := connErrorToOutput(err, payload.Alias)
		ipc.SendError(conn, ce.Hint, ce.Action)
		return
	}
	sendDaemonVerbose(conn, payload.Verbose,
		"connection: alias=%s duration=%s daemon reused=%t",
		payload.Alias, verboseDuration(time.Since(connectStart)), reused)

	profile := dc.getProfile(ctx, client, host.HostName, host.Port)
	sendDaemonVerbose(conn, payload.Verbose, "%s", verboseProfile(profile))
	shell := shellForExec(profile, payload.Shell)
	if shell == "" {
		sendDaemonVerbose(conn, payload.Verbose, "shell selected: default")
	} else {
		sendDaemonVerbose(conn, payload.Verbose, "shell selected: %s", shell)
	}

	start := time.Now()
	result, err := exec.RunBufferedWithShell(ctx, client, payload.Command, shell)
	durationMs := time.Since(start).Milliseconds()
	if err != nil {
		entry := audit.ExecErrorEntry(payload.Alias, payload.Command, audit.ResultError, durationMs, audit.SourceDaemon, err)
		if !dc.sendAudit(conn, entry) {
			return
		}
		ipc.SendError(conn, err.Error(), "")
		return
	}
	if remote.NeedsTranscoding(profile) {
		result.Stdout = remote.DecodeString(result.Stdout, profile.Encoding)
		result.Stderr = remote.DecodeString(result.Stderr, profile.Encoding)
	}
	auditResult := audit.ResultSuccess
	if result.ExitCode != 0 {
		auditResult = audit.ResultError
	}
	if !dc.sendAudit(conn, audit.ExecEntry(payload.Alias, payload.Command, auditResult, result.ExitCode, durationMs, audit.SourceDaemon)) {
		return
	}
	sendExecResultFrames(conn, result)
}

func sendExecResultFrames(conn net.Conn, result *exec.Result) {
	if result.Stdout != "" {
		ipc.Send(conn, ipc.Frame{Type: "stdout", Data: result.Stdout})
	}
	if result.Stderr != "" {
		ipc.Send(conn, ipc.Frame{Type: "stderr", Data: result.Stderr})
	}
	ipc.Send(conn, ipc.Frame{Type: "exit", Code: result.ExitCode})
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
