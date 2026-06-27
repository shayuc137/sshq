package cli

import (
	"context"
	"encoding/json"
	"net"
	"time"

	"github.com/shayuc137/sshq/internal/ipc"
	"github.com/shayuc137/sshq/internal/tunnel"
)

func (dc *daemonContext) handleTunnelStart(conn net.Conn, raw json.RawMessage) {
	var payload ipc.TunnelStartPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		ipc.SendError(conn, "invalid tunnel-start payload: "+err.Error(), "")
		return
	}

	cfg, ok := dc.resolveHost(conn, payload.Alias)
	if !ok {
		return
	}
	cfg.Timeout = 30 * time.Second

	client, ok := dc.getClient(conn, payload.Alias, cfg)
	if !ok {
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
		ipc.SendError(conn, "invalid direction: "+payload.Direction, "use 'local' or 'remote'")
		return
	}

	if err != nil {
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
	frame, _ := ipc.MakeResultFrame(result)
	ipc.Send(conn, frame)
}

func (dc *daemonContext) handleTunnelStop(conn net.Conn, raw json.RawMessage) {
	var payload ipc.TunnelStopPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		ipc.SendError(conn, "invalid tunnel-stop payload: "+err.Error(), "")
		return
	}

	if err := dc.tunnels.Stop(payload.ID); err != nil {
		ipc.SendError(conn, err.Error(), "use 'sshq tunnel list' to see active tunnels")
		return
	}

	frame, _ := ipc.MakeResultFrame(map[string]string{"stopped": payload.ID})
	ipc.Send(conn, frame)
}

func (dc *daemonContext) handleTunnelList(conn net.Conn) {
	list := dc.tunnels.List()
	frame, _ := ipc.MakeResultFrame(list)
	ipc.Send(conn, frame)
}
