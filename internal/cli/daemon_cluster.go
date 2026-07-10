package cli

import (
	"context"
	"encoding/json"
	"net"
	"sync"
	"time"

	"github.com/shayuc137/sshq/internal/audit"
	"github.com/shayuc137/sshq/internal/exec"
	"github.com/shayuc137/sshq/internal/ipc"
)

func (dc *daemonContext) handleClusterExec(conn net.Conn, raw json.RawMessage) {
	var payload ipc.ClusterExecPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		ipc.SendError(conn, "invalid cluster-exec payload: "+err.Error(), "")
		return
	}

	if len(payload.Aliases) == 0 {
		ipc.SendError(conn, "no hosts matched the filter", "use --tag, --env, or --all")
		return
	}
	if !dc.checkDaemonCluster(conn, payload.Aliases, payload.Command) {
		return
	}

	concurrency := payload.Concurrency
	if concurrency <= 0 {
		concurrency = 10
	}

	timeout := time.Duration(payload.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	success, failed := 0, 0
	auditStart := time.Now()
	auditFailed := false

	for _, alias := range payload.Aliases {
		wg.Add(1)
		sem <- struct{}{}
		go func(alias string) {
			defer wg.Done()
			defer func() { <-sem }()
			hostStart := time.Now()

			host, err := dc.store.Get(alias)
			if err != nil {
				mu.Lock()
				failed++
				mu.Unlock()
				entry := audit.ExecErrorEntry(alias, payload.Command, audit.ResultError, time.Since(hostStart).Milliseconds(), audit.SourceDaemon, err)
				entry.Operation = audit.OperationClusterExec
				entry.Aliases = append([]string(nil), payload.Aliases...)
				if !dc.sendAuditLocked(conn, &mu, entry) {
					mu.Lock()
					auditFailed = true
					mu.Unlock()
					return
				}
				sendClusterFrame(conn, &mu, ipc.ClusterFrame{Alias: alias, Type: "error", Hint: "host not found"})
				return
			}

			cfg, credErr := hostToConnConfigWithCredentials(host, dc.store, dc.creds)
			if credErr != nil {
				mu.Lock()
				failed++
				mu.Unlock()
				entry := audit.ExecErrorEntry(alias, payload.Command, audit.ResultError, time.Since(hostStart).Milliseconds(), audit.SourceDaemon, credErr)
				entry.Operation = audit.OperationClusterExec
				entry.Aliases = append([]string(nil), payload.Aliases...)
				if !dc.sendAuditLocked(conn, &mu, entry) {
					mu.Lock()
					auditFailed = true
					mu.Unlock()
					return
				}
				sendClusterFrame(conn, &mu, ipc.ClusterFrame{Alias: alias, Type: "error", Hint: credentialErrorSummary(credErr)})
				return
			}
			cfg.Timeout = timeout

			connectStart := time.Now()
			client, reused, cerr := dc.pool.GetWithStatus(context.Background(), alias, cfg)
			if cerr != nil {
				mu.Lock()
				failed++
				mu.Unlock()
				entry := audit.ExecErrorEntry(alias, payload.Command, audit.ResultError, time.Since(hostStart).Milliseconds(), audit.SourceDaemon, cerr)
				entry.Operation = audit.OperationClusterExec
				entry.Aliases = append([]string(nil), payload.Aliases...)
				if !dc.sendAuditLocked(conn, &mu, entry) {
					mu.Lock()
					auditFailed = true
					mu.Unlock()
					return
				}
				sendClusterFrame(conn, &mu, ipc.ClusterFrame{Alias: alias, Type: "error", Hint: cerr.Error()})
				return
			}
			sendDaemonVerboseLocked(conn, &mu, payload.Verbose,
				"connection: alias=%s duration=%s daemon reused=%t",
				alias, verboseDuration(time.Since(connectStart)), reused)

			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			profile := dc.getProfile(ctx, client, host.HostName, host.Port)
			sendDaemonVerboseLocked(conn, &mu, payload.Verbose, "%s", verboseProfile(profile))
			shell := shellForExec(profile, "")
			result, err := exec.RunBufferedWithShell(ctx, client, payload.Command, shell)
			if err != nil {
				mu.Lock()
				failed++
				mu.Unlock()
				entry := audit.ExecErrorEntry(alias, payload.Command, audit.ResultError, time.Since(hostStart).Milliseconds(), audit.SourceDaemon, err)
				entry.Operation = audit.OperationClusterExec
				entry.Aliases = append([]string(nil), payload.Aliases...)
				if !dc.sendAuditLocked(conn, &mu, entry) {
					mu.Lock()
					auditFailed = true
					mu.Unlock()
					return
				}
				sendClusterFrame(conn, &mu, ipc.ClusterFrame{Alias: alias, Type: "error", Hint: err.Error()})
				return
			}
			normalizeRemoteResult(result, profile, shell)

			auditResult := audit.ResultSuccess
			if result.ExitCode != 0 {
				auditResult = audit.ResultError
			}
			entry := audit.ExecEntry(alias, payload.Command, auditResult, result.ExitCode, time.Since(hostStart).Milliseconds(), audit.SourceDaemon)
			entry.Operation = audit.OperationClusterExec
			entry.Aliases = append([]string(nil), payload.Aliases...)
			if !dc.sendAuditLocked(conn, &mu, entry) {
				mu.Lock()
				auditFailed = true
				mu.Unlock()
				return
			}

			stdout := trimTrailingNewline(result.Stdout)
			if stdout != "" {
				sendClusterFrame(conn, &mu, ipc.ClusterFrame{Alias: alias, Type: "stdout", Data: stdout})
			}
			stderr := trimTrailingNewline(result.Stderr)
			if stderr != "" {
				sendClusterFrame(conn, &mu, ipc.ClusterFrame{Alias: alias, Type: "stderr", Data: stderr})
			}

			sendClusterFrame(conn, &mu, ipc.ClusterFrame{Alias: alias, Type: "exit", Code: result.ExitCode})

			mu.Lock()
			if result.ExitCode == 0 {
				success++
			} else {
				failed++
			}
			mu.Unlock()
		}(alias)
	}

	wg.Wait()
	mu.Lock()
	if auditFailed {
		mu.Unlock()
		return
	}
	mu.Unlock()

	summary := ipc.ClusterSummary{
		Total:   len(payload.Aliases),
		Success: success,
		Failed:  failed,
	}
	auditResult := audit.ResultSuccess
	if failed > 0 {
		auditResult = audit.ResultError
	}
	if !dc.sendAuditLocked(conn, &mu, audit.ClusterEntry(payload.Aliases, payload.Command, auditResult, time.Since(auditStart).Milliseconds(), audit.SourceDaemon)) {
		return
	}
	frame, _ := ipc.MakeResultFrame(summary)
	mu.Lock()
	ipc.Send(conn, frame)
	mu.Unlock()
}

func sendClusterFrame(conn net.Conn, mu *sync.Mutex, cf ipc.ClusterFrame) {
	b, _ := json.Marshal(cf)
	mu.Lock()
	ipc.Send(conn, ipc.Frame{Type: "cluster", Payload: json.RawMessage(b)})
	mu.Unlock()
}

func (dc *daemonContext) sendAuditLocked(conn net.Conn, mu *sync.Mutex, entry audit.Entry) bool {
	logger, enabled := dc.auditTarget()
	if !enabled {
		return true
	}
	if logger == nil {
		mu.Lock()
		ipc.SendError(conn, "audit log unavailable", "fix [audit] path or disable audit.enabled, then restart daemon")
		mu.Unlock()
		return false
	}
	if err := logger.Record(entry); err != nil {
		mu.Lock()
		ipc.SendError(conn, "audit log write failed: "+err.Error(), "check [audit] path and permissions")
		mu.Unlock()
		return false
	}
	return true
}

func trimTrailingNewline(s string) string {
	if len(s) > 0 && s[len(s)-1] == '\n' {
		s = s[:len(s)-1]
	}
	return s
}
