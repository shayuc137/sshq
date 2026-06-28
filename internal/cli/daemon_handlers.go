package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/shayuc137/sshq/internal/exec"
	"github.com/shayuc137/sshq/internal/ipc"
	"github.com/shayuc137/sshq/internal/remote"
	"github.com/shayuc137/sshq/internal/sshclient"
	"github.com/shayuc137/sshq/internal/transfer"
)

func (dc *daemonContext) resolveHost(conn net.Conn, alias string) (*sshclient.ConnConfig, bool) {
	host, err := dc.store.Get(alias)
	if err != nil {
		ipc.SendError(conn, err.Error(), "run 'sshq ls' to see available hosts")
		return nil, false
	}
	c := hostToConnConfigWithStore(host, dc.store)
	cfg := &c
	return cfg, true
}

func (dc *daemonContext) getClient(conn net.Conn, alias string, cfg *sshclient.ConnConfig) (*sshclient.Client, bool) {
	return dc.getClientWithContext(context.Background(), conn, alias, cfg)
}

func (dc *daemonContext) getClientWithContext(ctx context.Context, conn net.Conn, alias string, cfg *sshclient.ConnConfig) (*sshclient.Client, bool) {
	client, err := dc.pool.Get(ctx, alias, *cfg)
	if err != nil {
		ce := connErrorToOutput(err, alias)
		ipc.SendError(conn, ce.Hint, ce.Action)
		return nil, false
	}
	return client, true
}

func (dc *daemonContext) getProfile(ctx context.Context, client *sshclient.Client, hostName, port string) *remote.Profile {
	p, _ := remote.GetProfile(ctx, client, dc.cache, hostName, port)
	return p
}

// --- script handler ---

func (dc *daemonContext) handleScript(conn net.Conn, raw json.RawMessage) {
	var payload ipc.ScriptPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		ipc.SendError(conn, "invalid script payload: "+err.Error(), "")
		return
	}

	cfg, ok := dc.resolveHost(conn, payload.Alias)
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

	client, ok := dc.getClientWithContext(ctx, conn, payload.Alias, cfg)
	if !ok {
		return
	}

	profile := dc.getProfile(ctx, client, cfg.Host, cfg.Port)
	shell := shellForExec(profile, payload.Shell)
	if shell == "" {
		shell = "sh"
	}

	result, err := exec.RunScriptBuffered(ctx, client, payload.Script, shell)
	if err != nil {
		ipc.SendError(conn, err.Error(), "")
		return
	}
	if remote.NeedsTranscoding(profile) {
		result.Stdout = remote.DecodeString(result.Stdout, profile.Encoding)
		result.Stderr = remote.DecodeString(result.Stderr, profile.Encoding)
	}

	sendExecResultFrames(conn, result)
}

// --- transfer handler ---

func (dc *daemonContext) handleTransfer(conn net.Conn, raw json.RawMessage) {
	var payload ipc.TransferPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		ipc.SendError(conn, "invalid transfer payload: "+err.Error(), "")
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

	profile := dc.getProfile(context.Background(), client, cfg.Host, cfg.Port)
	infoFn := func(msg string) {
		ipc.Send(conn, ipc.Frame{Type: "stderr", Data: msg + "\n"})
	}

	engine, err := transfer.NewEngine(client, profile, infoFn)
	if err != nil {
		ipc.SendError(conn, "transfer engine: "+err.Error(), "")
		return
	}
	defer engine.Close()

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
		ipc.SendError(conn, "invalid direction: "+payload.Direction, "use 'upload' or 'download'")
		return
	}

	if err != nil {
		ipc.SendError(conn, err.Error(), "")
		return
	}

	frame, _ := ipc.MakeResultFrame(result)
	ipc.Send(conn, frame)
}

// --- relay handler ---

func (dc *daemonContext) handleRelay(conn net.Conn, raw json.RawMessage) {
	var payload ipc.RelayPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		ipc.SendError(conn, "invalid relay payload: "+err.Error(), "")
		return
	}

	srcCfg, ok := dc.resolveHost(conn, payload.SrcAlias)
	if !ok {
		return
	}
	srcCfg.Timeout = 30 * time.Second

	dstCfg, ok := dc.resolveHost(conn, payload.DstAlias)
	if !ok {
		return
	}
	dstCfg.Timeout = 30 * time.Second

	srcClient, ok := dc.getClient(conn, payload.SrcAlias, srcCfg)
	if !ok {
		return
	}
	dstClient, ok := dc.getClient(conn, payload.DstAlias, dstCfg)
	if !ok {
		return
	}

	srcProfile := dc.getProfile(context.Background(), srcClient, srcCfg.Host, srcCfg.Port)
	dstProfile := dc.getProfile(context.Background(), dstClient, dstCfg.Host, dstCfg.Port)

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
		result, err = transfer.RunRelayRecursive(ctx, srcClient, dstClient, payload.SrcPath, payload.DstPath, srcProfile, dstProfile, infoFn, progressFn)
	} else {
		result, err = transfer.RunRelay(ctx, srcClient, dstClient, payload.SrcPath, payload.DstPath, srcProfile, dstProfile, infoFn, progressFn)
	}

	if err != nil {
		ipc.SendError(conn, err.Error(), "")
		return
	}

	frame, _ := ipc.MakeResultFrame(result)
	ipc.Send(conn, frame)
}

// --- profile handler ---

func (dc *daemonContext) handleProfile(conn net.Conn, raw json.RawMessage) {
	var payload ipc.ProfilePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		ipc.SendError(conn, "invalid profile payload: "+err.Error(), "")
		return
	}

	host, err := dc.store.Get(payload.Alias)
	if err != nil {
		ipc.SendError(conn, err.Error(), "run 'sshq ls' to see available hosts")
		return
	}

	if payload.Refresh && dc.cache != nil {
		dc.cache.Invalidate(host.HostName, host.Port)
	}

	if !payload.Refresh && dc.cache != nil {
		if cached, _ := dc.cache.Get(host.HostName, host.Port); cached != nil {
			result := ipc.ProfileResult{
				OS:       string(cached.OS),
				Shell:    string(cached.Shell),
				Encoding: cached.Encoding,
				HomeDir:  cached.HomeDir,
			}
			frame, _ := ipc.MakeResultFrame(result)
			ipc.Send(conn, frame)
			return
		}
	}

	cfg := hostToConnConfigWithStore(host, dc.store)
	cfg.Timeout = 30 * time.Second

	client, ok := dc.getClient(conn, payload.Alias, &cfg)
	if !ok {
		return
	}

	p, err := remote.GetProfile(context.Background(), client, dc.cache, host.HostName, host.Port)
	if err != nil {
		ipc.SendError(conn, fmt.Sprintf("profile detect failed: %s", err), "")
		return
	}

	result := ipc.ProfileResult{
		OS:       string(p.OS),
		Shell:    string(p.Shell),
		Encoding: p.Encoding,
		HomeDir:  p.HomeDir,
	}
	frame, _ := ipc.MakeResultFrame(result)
	ipc.Send(conn, frame)
}
