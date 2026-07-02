package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/shayuc137/sshq/internal/audit"
	"github.com/shayuc137/sshq/internal/ipc"
	"github.com/shayuc137/sshq/internal/tunnel"
)

func (dc *daemonContext) handleTunnelStart(conn net.Conn, raw json.RawMessage) {
	var payload ipc.TunnelStartPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		ipc.SendError(conn, "invalid tunnel-start payload: "+err.Error(), "")
		return
	}
	auditStart := time.Now()
	tunnelAuditErr := func(error) audit.Entry {
		return audit.TunnelEntry(payload.Alias, payload.Direction, payload.LocalAddr, payload.RemoteAddr, "start", audit.ResultError, time.Since(auditStart).Milliseconds(), audit.SourceDaemon)
	}

	cfg, ok := dc.resolveHost(conn, payload.Alias, tunnelAuditErr)
	if !ok {
		return
	}
	cfg.Timeout = 30 * time.Second

	connectStart := time.Now()
	client, reused, ok := dc.getClientWithStatus(context.Background(), conn, payload.Alias, cfg, tunnelAuditErr)
	if !ok {
		return
	}
	sendDaemonVerbose(conn, payload.Verbose,
		"connection: alias=%s duration=%s daemon reused=%t",
		payload.Alias, verboseDuration(time.Since(connectStart)), reused)

	var policyTarget string
	if payload.Direction == "local" {
		policyTarget = payload.RemoteAddr
	} else {
		policyTarget = payload.LocalAddr
	}
	if !dc.checkDaemonForward(conn, payload.Alias, payload.Direction, policyTarget, payload.LocalAddr, payload.RemoteAddr, auditStart) {
		return
	}

	tunnelCfg := tunnel.Config{
		Direction:  tunnel.Direction(payload.Direction),
		Alias:      payload.Alias,
		LocalAddr:  payload.LocalAddr,
		RemoteAddr: payload.RemoteAddr,
	}

	ctx := context.Background()
	var t *tunnel.Tunnel
	var err error

	switch tunnelCfg.Direction {
	case tunnel.Local:
		t, err = tunnel.StartLocal(ctx, client, tunnelCfg, nil)
	case tunnel.Remote:
		t, err = tunnel.StartRemote(ctx, client, tunnelCfg, nil)
	default:
		err := fmt.Errorf("invalid direction: %s", payload.Direction)
		entry := audit.TunnelEntry(payload.Alias, payload.Direction, payload.LocalAddr, payload.RemoteAddr, "start", audit.ResultError, time.Since(auditStart).Milliseconds(), audit.SourceDaemon)
		if !dc.sendAuditError(conn, entry, err) {
			return
		}
		ipc.SendError(conn, "invalid direction: "+payload.Direction, "use 'local' or 'remote'")
		return
	}

	if err != nil {
		entry := audit.TunnelEntry(payload.Alias, payload.Direction, payload.LocalAddr, payload.RemoteAddr, "start", audit.ResultError, time.Since(auditStart).Milliseconds(), audit.SourceDaemon)
		if !dc.sendAuditError(conn, entry, err) {
			return
		}
		ipc.SendError(conn, err.Error(), "")
		return
	}

	dc.tunnels.Add(t)

	result := ipc.TunnelStartResult{
		ID:         t.ID,
		Direction:  payload.Direction,
		LocalAddr:  payload.LocalAddr,
		RemoteAddr: payload.RemoteAddr,
	}
	if !dc.sendAudit(conn, audit.TunnelEntry(payload.Alias, payload.Direction, payload.LocalAddr, payload.RemoteAddr, "start", audit.ResultSuccess, time.Since(auditStart).Milliseconds(), audit.SourceDaemon)) {
		dc.tunnels.Stop(t.ID)
		return
	}
	frame, _ := ipc.MakeResultFrame(result)
	ipc.Send(conn, frame)
}

func (dc *daemonContext) handleTunnelStop(conn net.Conn, raw json.RawMessage) {
	var payload ipc.TunnelStopPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		ipc.SendError(conn, "invalid tunnel-stop payload: "+err.Error(), "")
		return
	}

	auditStart := time.Now()
	active, found := dc.tunnels.Get(payload.ID)
	if err := dc.tunnels.Stop(payload.ID); err != nil {
		entry := audit.TunnelEntry("", "", "", "", "stop", audit.ResultError, time.Since(auditStart).Milliseconds(), audit.SourceDaemon)
		entry.Summary = audit.RedactSummary("stop " + payload.ID)
		if !dc.sendAuditError(conn, entry, err) {
			return
		}
		ipc.SendError(conn, err.Error(), "use 'sshq tunnel list' to see active tunnels")
		return
	}

	if found {
		cfg := active.Config
		if !dc.sendAudit(conn, audit.TunnelEntry(cfg.Alias, string(cfg.Direction), cfg.LocalAddr, cfg.RemoteAddr, "stop", audit.ResultSuccess, time.Since(auditStart).Milliseconds(), audit.SourceDaemon)) {
			return
		}
	} else {
		if !dc.sendAudit(conn, audit.TunnelEntry("", "", "", "", "stop", audit.ResultSuccess, time.Since(auditStart).Milliseconds(), audit.SourceDaemon)) {
			return
		}
	}
	frame, _ := ipc.MakeResultFrame(map[string]string{"stopped": payload.ID})
	ipc.Send(conn, frame)
}

func (dc *daemonContext) handleTunnelList(conn net.Conn) {
	list := dc.tunnels.List()
	frame, _ := ipc.MakeResultFrame(list)
	ipc.Send(conn, frame)
}
