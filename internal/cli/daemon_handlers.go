package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/shayuc137/sshq/internal/audit"
	"github.com/shayuc137/sshq/internal/exec"
	"github.com/shayuc137/sshq/internal/ipc"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/remote"
	"github.com/shayuc137/sshq/internal/sshclient"
	"github.com/shayuc137/sshq/internal/transfer"
)

// auditErrFn lets a handler turn a pre-execution failure into an audit entry
// before the IPC error is sent. It returns the operation-specific error entry
// for err. A nil auditErrFn (e.g. the non-audited profile handler) skips audit
// recording entirely.
type auditErrFn func(err error) audit.Entry

// resolveHost looks up an alias and builds its ConnConfig with credentials.
// On alias-resolution or credential failure it records an audit entry (when
// auditErr is non-nil) before sending the IPC error, so daemon error paths are
// audited the same way the direct paths are. Returns (cfg, true) on success.
func (dc *daemonContext) resolveHost(conn net.Conn, alias string, auditErr auditErrFn) (*sshclient.ConnConfig, bool) {
	store := dc.storeSnapshot()
	host, err := store.Get(alias)
	if err != nil {
		if !dc.recordResolveAudit(conn, auditErr, err) {
			return nil, false
		}
		ipc.SendError(conn, output.CodeHostNotFound, err.Error(), "run 'sshq ls' to see available hosts")
		return nil, false
	}
	c, credErr := hostToConnConfigWithCredentials(host, store, dc.creds)
	if credErr != nil {
		if !dc.recordResolveAudit(conn, auditErr, credErr) {
			return nil, false
		}
		ipc.SendError(conn, output.CodeCredentialError, credentialErrorSummary(credErr), "check credential store integrity and permissions")
		return nil, false
	}
	cfg := &c
	return cfg, true
}

func (dc *daemonContext) recordResolveAudit(conn net.Conn, auditErr auditErrFn, err error) bool {
	if auditErr == nil {
		return true
	}
	return dc.sendAuditError(conn, auditErr(err), err)
}

// getClientWithStatus dials (or reuses) a pooled client. On failure it records
// an audit entry (when auditErr is non-nil) before sending the IPC error.
func (dc *daemonContext) getClientWithStatus(ctx context.Context, conn net.Conn, alias string, cfg *sshclient.ConnConfig, auditErr auditErrFn) (*sshclient.Client, bool, bool) {
	client, reused, err := dc.pool.GetWithStatus(ctx, alias, *cfg)
	if err != nil {
		if !dc.recordResolveAudit(conn, auditErr, err) {
			return nil, false, false
		}
		ce := connErrorToOutput(err, alias)
		ipc.SendError(conn, ce.Code, ce.Hint, ce.Action)
		return nil, false, false
	}
	return client, reused, true
}

func (dc *daemonContext) getProfile(ctx context.Context, client *sshclient.Client, hostName, port string) *remote.Profile {
	p, _ := remote.GetProfile(ctx, client, dc.cache, hostName, port)
	return p
}

// --- script handler ---

func (dc *daemonContext) handleScript(conn net.Conn, raw json.RawMessage) {
	var payload ipc.ScriptPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		ipc.SendError(conn, output.CodeInvalidUsage, "invalid script payload: "+err.Error(), "")
		return
	}
	if !dc.checkDaemonCommandWithAudit(conn, payload.Alias, string(payload.Script), audit.OperationExec, audit.ScriptSummary(payload.Script)) {
		return
	}

	auditStart := time.Now()
	scriptAuditErr := func(err error) audit.Entry {
		return audit.ScriptErrorEntry(payload.Alias, payload.Script, audit.ResultError, time.Since(auditStart).Milliseconds(), audit.SourceDaemon, err)
	}

	cfg, ok := dc.resolveHost(conn, payload.Alias, scriptAuditErr)
	if !ok {
		return
	}
	timeout := time.Duration(payload.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	cfg.Timeout = timeout

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	connectStart := time.Now()
	client, reused, ok := dc.getClientWithStatus(ctx, conn, payload.Alias, cfg, scriptAuditErr)
	if !ok {
		return
	}
	sendDaemonVerbose(conn, payload.Verbose,
		"connection: alias=%s duration=%s daemon reused=%t",
		payload.Alias, verboseDuration(time.Since(connectStart)), reused)

	profile := dc.getProfile(ctx, client, cfg.Host, cfg.Port)
	sendDaemonVerbose(conn, payload.Verbose, "%s", verboseProfile(profile))
	shell := shellForExec(profile, payload.Shell)
	if shell == "" {
		shell = "sh"
	}
	sendDaemonVerbose(conn, payload.Verbose, "shell selected: %s", shell)

	start := time.Now()
	result, err := exec.RunScriptBuffered(ctx, client, payload.Script, shell,
		exec.WithRemoteProfile(profile),
		exec.WithScriptVerbose(func(msg string) {
			sendDaemonVerbose(conn, payload.Verbose, "%s", msg)
		}),
	)
	durationMs := time.Since(start).Milliseconds()
	if err != nil {
		entry := audit.ScriptErrorEntry(payload.Alias, payload.Script, audit.ResultError, durationMs, audit.SourceDaemon, err)
		if !dc.sendAudit(conn, entry) {
			return
		}
		ipc.SendError(conn, output.CodeResultIndeterminate, err.Error(), "")
		return
	}
	normalizeRemoteResult(result, profile, shell)

	auditResult := audit.ResultSuccess
	if result.ExitCode != 0 {
		auditResult = audit.ResultError
	}
	if !dc.sendAudit(conn, audit.ScriptEntry(payload.Alias, payload.Script, auditResult, result.ExitCode, durationMs, audit.SourceDaemon)) {
		return
	}
	sendExecResultFrames(conn, result)
}

// --- transfer handler ---

func (dc *daemonContext) handleTransfer(conn net.Conn, raw json.RawMessage) {
	var payload ipc.TransferPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		ipc.SendError(conn, output.CodeInvalidUsage, "invalid transfer payload: "+err.Error(), "")
		return
	}
	if !dc.checkDaemonTransfer(conn, payload) {
		return
	}
	alias, direction, localPath, remotePath := transferPayloadAuditParts(payload)
	auditStart := time.Now()
	transferAuditErr := func(error) audit.Entry {
		return audit.TransferEntry(alias, direction, localPath, remotePath, audit.ResultError, time.Since(auditStart).Milliseconds(), audit.SourceDaemon)
	}

	cfg, ok := dc.resolveHost(conn, payload.Alias, transferAuditErr)
	if !ok {
		return
	}
	cfg.Timeout = 30 * time.Second

	connectStart := time.Now()
	client, reused, ok := dc.getClientWithStatus(context.Background(), conn, payload.Alias, cfg, transferAuditErr)
	if !ok {
		return
	}
	sendDaemonVerbose(conn, payload.Verbose,
		"connection: alias=%s duration=%s daemon reused=%t",
		payload.Alias, verboseDuration(time.Since(connectStart)), reused)

	profile := dc.getProfile(context.Background(), client, cfg.Host, cfg.Port)
	sendDaemonVerbose(conn, payload.Verbose, "%s", verboseProfile(profile))
	infoFn := func(msg string) {
		ipc.Send(conn, ipc.Frame{Type: "stderr", Data: msg + "\n"})
	}

	engine, err := transfer.NewEngine(client, profile, infoFn, transferEngineOptions(payload.Mkdirs)...)
	if err != nil {
		entry := audit.TransferEntry(alias, direction, localPath, remotePath, audit.ResultError, time.Since(auditStart).Milliseconds(), audit.SourceDaemon)
		if !dc.sendAuditError(conn, entry, err) {
			return
		}
		ipc.SendError(conn, output.CodeTransferFailed, "transfer engine: "+err.Error(), "")
		return
	}
	defer engine.Close()
	sendDaemonVerbose(conn, payload.Verbose, "transfer engine: %s", engine.Name())

	progressFn := func(info transfer.ProgressInfo) {
		b, _ := json.Marshal(info)
		ipc.Send(conn, ipc.Frame{Type: "progress", Payload: json.RawMessage(b)})
	}

	ctx := context.Background()
	var result *transfer.Result

	switch payload.Direction {
	case "upload":
		if payload.Recursive {
			result, err = engine.UploadRecursive(ctx, payload.LocalPath, payload.RemotePath, progressFn)
		} else {
			result, err = engine.Upload(ctx, payload.LocalPath, payload.RemotePath, progressFn)
		}
	case "download":
		if payload.Recursive {
			result, err = engine.DownloadRecursive(ctx, payload.RemotePath, payload.LocalPath, progressFn)
		} else {
			result, err = engine.Download(ctx, payload.RemotePath, payload.LocalPath, progressFn)
		}
	default:
		err := fmt.Errorf("invalid direction: %s", payload.Direction)
		entry := audit.TransferEntry(alias, direction, localPath, remotePath, audit.ResultError, time.Since(auditStart).Milliseconds(), audit.SourceDaemon)
		if !dc.sendAuditError(conn, entry, err) {
			return
		}
		ipc.SendError(conn, output.CodeInvalidUsage, "invalid direction: "+payload.Direction, "use 'upload' or 'download'")
		return
	}

	if err != nil {
		entry := audit.TransferEntry(alias, direction, localPath, remotePath, audit.ResultError, time.Since(auditStart).Milliseconds(), audit.SourceDaemon)
		if !dc.sendAuditError(conn, entry, err) {
			return
		}
		action := ""
		var missingParent *transfer.RemoteParentMissingError
		if errors.As(err, &missingParent) {
			action = cpMkdirsAction(parsedArgsFromTransferPayload(payload), payload.Recursive)
		}
		ipc.SendError(conn, output.CodeTransferFailed, err.Error(), action)
		return
	}

	if !dc.sendAudit(conn, audit.TransferEntry(alias, direction, localPath, remotePath, audit.ResultSuccess, time.Since(auditStart).Milliseconds(), audit.SourceDaemon)) {
		return
	}
	frame, _ := ipc.MakeResultFrame(result)
	ipc.Send(conn, frame)
}

// --- relay handler ---

func (dc *daemonContext) handleRelay(conn net.Conn, raw json.RawMessage) {
	var payload ipc.RelayPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		ipc.SendError(conn, output.CodeInvalidUsage, "invalid relay payload: "+err.Error(), "")
		return
	}
	if !dc.checkDaemonRelay(conn, payload) {
		return
	}
	auditStart := time.Now()
	relayAuditErr := func(error) audit.Entry {
		return audit.RelayEntry(payload.SrcAlias, payload.SrcPath, payload.DstAlias, payload.DstPath, audit.ResultError, time.Since(auditStart).Milliseconds(), audit.SourceDaemon)
	}

	srcCfg, ok := dc.resolveHost(conn, payload.SrcAlias, relayAuditErr)
	if !ok {
		return
	}
	srcCfg.Timeout = 30 * time.Second

	dstCfg, ok := dc.resolveHost(conn, payload.DstAlias, relayAuditErr)
	if !ok {
		return
	}
	dstCfg.Timeout = 30 * time.Second

	srcConnectStart := time.Now()
	srcClient, srcReused, ok := dc.getClientWithStatus(context.Background(), conn, payload.SrcAlias, srcCfg, relayAuditErr)
	if !ok {
		return
	}
	sendDaemonVerbose(conn, payload.Verbose,
		"connection: alias=%s duration=%s daemon reused=%t",
		payload.SrcAlias, verboseDuration(time.Since(srcConnectStart)), srcReused)
	dstConnectStart := time.Now()
	dstClient, dstReused, ok := dc.getClientWithStatus(context.Background(), conn, payload.DstAlias, dstCfg, relayAuditErr)
	if !ok {
		return
	}
	sendDaemonVerbose(conn, payload.Verbose,
		"connection: alias=%s duration=%s daemon reused=%t",
		payload.DstAlias, verboseDuration(time.Since(dstConnectStart)), dstReused)

	srcProfile := dc.getProfile(context.Background(), srcClient, srcCfg.Host, srcCfg.Port)
	sendDaemonVerbose(conn, payload.Verbose, "source %s", verboseProfile(srcProfile))
	dstProfile := dc.getProfile(context.Background(), dstClient, dstCfg.Host, dstCfg.Port)
	sendDaemonVerbose(conn, payload.Verbose, "destination %s", verboseProfile(dstProfile))

	infoFn := func(msg string) {
		ipc.Send(conn, ipc.Frame{Type: "stderr", Data: msg + "\n"})
	}
	progressFn := func(info transfer.ProgressInfo) {
		b, _ := json.Marshal(info)
		ipc.Send(conn, ipc.Frame{Type: "progress", Payload: json.RawMessage(b)})
	}

	ctx := context.Background()
	var result *transfer.Result
	var err error

	if payload.Recursive {
		result, err = transfer.RunRelayRecursive(ctx, srcClient, dstClient, payload.SrcPath, payload.DstPath, srcProfile, dstProfile, infoFn, progressFn, transferEngineOptions(payload.Mkdirs)...)
	} else {
		result, err = transfer.RunRelay(ctx, srcClient, dstClient, payload.SrcPath, payload.DstPath, srcProfile, dstProfile, infoFn, progressFn, transferEngineOptions(payload.Mkdirs)...)
	}

	if err != nil {
		entry := audit.RelayEntry(payload.SrcAlias, payload.SrcPath, payload.DstAlias, payload.DstPath, audit.ResultError, time.Since(auditStart).Milliseconds(), audit.SourceDaemon)
		if !dc.sendAuditError(conn, entry, err) {
			return
		}
		action := ""
		var missingParent *transfer.RemoteParentMissingError
		if errors.As(err, &missingParent) {
			action = cpMkdirsAction(parsedArgsFromRelayPayload(payload), payload.Recursive)
		}
		ipc.SendError(conn, output.CodeTransferFailed, err.Error(), action)
		return
	}
	sendDaemonVerbose(conn, payload.Verbose, "transfer engine: %s", result.Engine)

	if !dc.sendAudit(conn, audit.RelayEntry(payload.SrcAlias, payload.SrcPath, payload.DstAlias, payload.DstPath, audit.ResultSuccess, time.Since(auditStart).Milliseconds(), audit.SourceDaemon)) {
		return
	}
	frame, _ := ipc.MakeResultFrame(result)
	ipc.Send(conn, frame)
}

// --- profile handler ---

func (dc *daemonContext) handleProfile(conn net.Conn, raw json.RawMessage) {
	var payload ipc.ProfilePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		ipc.SendError(conn, output.CodeInvalidUsage, "invalid profile payload: "+err.Error(), "")
		return
	}

	store := dc.storeSnapshot()
	host, err := store.Get(payload.Alias)
	if err != nil {
		ipc.SendError(conn, output.CodeHostNotFound, err.Error(), "run 'sshq ls' to see available hosts")
		return
	}

	if payload.Refresh && dc.cache != nil {
		dc.cache.Invalidate(host.HostName, host.Port)
	}

	if !payload.Refresh && dc.cache != nil {
		if cached, _ := dc.cache.Get(host.HostName, host.Port); cached != nil {
			sendDaemonVerbose(conn, payload.Verbose, "%s", verboseProfile(cached))
			result := ipc.ProfileResult{
				OS:             string(cached.OS),
				Shell:          string(cached.Shell),
				Encoding:       cached.Encoding,
				HomeDir:        cached.HomeDir,
				TempDir:        cached.TempDir,
				PowerShellPath: cached.PowerShellPath,
				PwshPath:       cached.PwshPath,
			}
			frame, _ := ipc.MakeResultFrame(result)
			ipc.Send(conn, frame)
			return
		}
	}

	cfg, credErr := hostToConnConfigWithCredentials(host, store, dc.creds)
	if credErr != nil {
		ipc.SendError(conn, output.CodeCredentialError, credentialErrorSummary(credErr), "check credential store integrity and permissions")
		return
	}
	cfg.Timeout = 30 * time.Second

	connectStart := time.Now()
	client, reused, ok := dc.getClientWithStatus(context.Background(), conn, payload.Alias, &cfg, nil)
	if !ok {
		return
	}
	sendDaemonVerbose(conn, payload.Verbose,
		"connection: alias=%s duration=%s daemon reused=%t",
		payload.Alias, verboseDuration(time.Since(connectStart)), reused)

	p, err := remote.GetProfile(context.Background(), client, dc.cache, host.HostName, host.Port)
	if err != nil {
		ipc.SendError(conn, output.CodeNetworkError, fmt.Sprintf("profile detect failed: %s", err), "")
		return
	}
	sendDaemonVerbose(conn, payload.Verbose, "%s", verboseProfile(p))

	result := ipc.ProfileResult{
		OS:             string(p.OS),
		Shell:          string(p.Shell),
		Encoding:       p.Encoding,
		HomeDir:        p.HomeDir,
		TempDir:        p.TempDir,
		PowerShellPath: p.PowerShellPath,
		PwshPath:       p.PwshPath,
	}
	frame, _ := ipc.MakeResultFrame(result)
	ipc.Send(conn, frame)
}
