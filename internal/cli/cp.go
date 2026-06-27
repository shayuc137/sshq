package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/shayuc137/sshq/internal/config"
	"github.com/shayuc137/sshq/internal/ipc"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/remote"
	"github.com/shayuc137/sshq/internal/sshclient"
	"github.com/shayuc137/sshq/internal/transfer"
	"github.com/spf13/cobra"
)

func newCpCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cp <src> <dst>",
		Short: "Copy files between local and remote hosts",
		Long: `Copy files using alias:path syntax to determine direction:
  sshq cp local.txt ali:/tmp/          upload
  sshq cp ali:/var/log/app.log ./      download
  sshq cp ali:/data/f.tar rn:/backup/  server-to-server relay`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			parsed, err := transfer.ParseArgs(args[0], args[1])
			if err != nil {
				return output.Errorf(err.Error(), "usage: sshq cp <src> <dst>")
			}

			store := configFrom(cmd.Context())
			if store == nil {
				return output.Errorf("no SSH config loaded", "check ~/.ssh/config exists")
			}

			w := writerFrom(cmd.Context())
			recursive, _ := cmd.Flags().GetBool("recursive")
			noProgress, _ := cmd.Flags().GetBool("no-progress")
			noDaemon, _ := cmd.Flags().GetBool("no-daemon")
			timeout, _ := cmd.Flags().GetDuration("timeout")

			var progressFn transfer.ProgressFunc
			if !noProgress {
				progressFn = makeProgressFunc(w)
			}

			ctx := cmd.Context()
			if timeout > 0 {
				var cancel func()
				ctx, cancel = context.WithTimeout(ctx, timeout)
				defer cancel()
			}

			if !noDaemon && ipc.IsRunning() {
				switch parsed.Direction {
				case transfer.Upload, transfer.Download:
					return cpTransferViaDaemon(cmd, w, parsed, recursive, noProgress)
				case transfer.Relay:
					return cpRelayViaDaemon(cmd, w, parsed, recursive, noProgress)
				}
			}

			switch parsed.Direction {
			case transfer.Upload, transfer.Download:
				return cpTransferDirect(ctx, w, store, parsed, recursive, progressFn)
			case transfer.Relay:
				return cpRelayDirect(ctx, w, store, parsed, recursive, progressFn)
			}
			return nil
		},
	}

	cmd.Flags().BoolP("recursive", "r", false, "copy directories recursively")
	cmd.Flags().Bool("no-progress", false, "disable progress output")
	cmd.Flags().Bool("no-daemon", false, "skip daemon, connect directly")
	return cmd
}

// --- daemon paths ---

func cpTransferViaDaemon(cmd *cobra.Command, w *output.Writer, parsed transfer.ParsedArgs, recursive, noProgress bool) error {
	conn, err := ipc.Connect()
	if err != nil {
		w.Info("daemon unreachable, falling back to direct connection")
		return cpTransferDirectFromCmd(cmd, w, parsed, recursive, noProgress)
	}
	defer conn.Close()

	alias := parsed.Src.Alias
	localPath := parsed.Src.Path
	remotePath := parsed.Dst.Path
	direction := "upload"
	if alias == "" {
		alias = parsed.Dst.Alias
		localPath = parsed.Src.Path
		remotePath = parsed.Dst.Path
		direction = "upload"
	}
	if parsed.Direction == transfer.Download {
		alias = parsed.Src.Alias
		localPath = parsed.Dst.Path
		remotePath = parsed.Src.Path
		direction = "download"
	}

	env, _ := ipc.MakeEnvelope("transfer", ipc.TransferPayload{
		Direction:  direction,
		Alias:      alias,
		LocalPath:  localPath,
		RemotePath: remotePath,
		Recursive:  recursive,
	})
	if err := ipc.Send(conn, env); err != nil {
		w.Info("daemon send failed, falling back to direct connection")
		return cpTransferDirectFromCmd(cmd, w, parsed, recursive, noProgress)
	}

	return recvTransferFrames(w, conn)
}

func cpRelayViaDaemon(cmd *cobra.Command, w *output.Writer, parsed transfer.ParsedArgs, recursive, noProgress bool) error {
	conn, err := ipc.Connect()
	if err != nil {
		w.Info("daemon unreachable, falling back to direct connection")
		return cpRelayDirectFromCmd(cmd, w, parsed, recursive, noProgress)
	}
	defer conn.Close()

	env, _ := ipc.MakeEnvelope("relay", ipc.RelayPayload{
		SrcAlias:  parsed.Src.Alias,
		SrcPath:   parsed.Src.Path,
		DstAlias:  parsed.Dst.Alias,
		DstPath:   parsed.Dst.Path,
		Recursive: recursive,
	})
	if err := ipc.Send(conn, env); err != nil {
		w.Info("daemon send failed, falling back to direct connection")
		return cpRelayDirectFromCmd(cmd, w, parsed, recursive, noProgress)
	}

	return recvTransferFrames(w, conn)
}

func recvTransferFrames(w *output.Writer, conn net.Conn) error {
	for {
		msg, err := ipc.Recv(conn)
		if err != nil {
			return output.Errorf("daemon connection lost", "retry or use --no-daemon")
		}

		var frame ipc.Frame
		if err := json.Unmarshal(msg, &frame); err != nil {
			return output.Errorf("invalid daemon response", "")
		}

		switch frame.Type {
		case "stderr":
			w.Info(frame.Data)
		case "progress":
			if !w.IsJSONMode() {
				var info transfer.ProgressInfo
				json.Unmarshal(frame.Payload, &info)
				w.Info(fmt.Sprintf("%s %d%% %s/%s %s",
					info.File, info.Percent,
					transfer.HumanSize(info.Transferred),
					transfer.HumanSize(info.Total),
					info.Speed))
			}
		case "result":
			var result transfer.Result
			json.Unmarshal(frame.Payload, &result)
			renderCpResult(w, &result)
			return nil
		case "error":
			return output.Errorf(frame.Hint, frame.Action)
		}
	}
}

// --- direct paths (fallback) ---

func cpTransferDirectFromCmd(cmd *cobra.Command, w *output.Writer, parsed transfer.ParsedArgs, recursive, noProgress bool) error {
	store := configFrom(cmd.Context())
	var progressFn transfer.ProgressFunc
	if !noProgress {
		progressFn = makeProgressFunc(w)
	}
	return cpTransferDirect(cmd.Context(), w, store, parsed, recursive, progressFn)
}

func cpRelayDirectFromCmd(cmd *cobra.Command, w *output.Writer, parsed transfer.ParsedArgs, recursive, noProgress bool) error {
	store := configFrom(cmd.Context())
	var progressFn transfer.ProgressFunc
	if !noProgress {
		progressFn = makeProgressFunc(w)
	}
	return cpRelayDirect(cmd.Context(), w, store, parsed, recursive, progressFn)
}

func cpTransferDirect(ctx context.Context, w *output.Writer, store *config.Store, parsed transfer.ParsedArgs, recursive bool, progress transfer.ProgressFunc) error {
	alias := parsed.Src.Alias
	if alias == "" {
		alias = parsed.Dst.Alias
	}

	host, err := store.Get(alias)
	if err != nil {
		return output.Errorf(err.Error(), "run 'sshq ls' to see available hosts")
	}

	cfg := sshclient.ConnConfig{
		Host:         host.HostName,
		Port:         host.Port,
		User:         host.User,
		IdentityFile: host.IdentityFile,
		Timeout:      30 * time.Second,
	}

	w.Info("connecting to " + alias + "...")
	client, err := sshclient.Dial(ctx, cfg)
	if err != nil {
		return connErrorToOutput(err, alias)
	}
	defer client.Close()

	cache := profileCacheFrom(ctx)
	profile, _ := remote.GetProfile(ctx, client, cache, host.HostName, host.Port)

	engine, err := transfer.NewEngine(client, profile, func(msg string) { w.Info(msg) })
	if err != nil {
		return output.Errorf("transfer engine: "+err.Error(), "")
	}
	defer engine.Close()

	var result *transfer.Result

	switch parsed.Direction {
	case transfer.Upload:
		if recursive {
			result, err = engine.UploadRecursive(ctx, parsed.Src.Path, parsed.Dst.Path, progress)
		} else {
			result, err = engine.Upload(ctx, parsed.Src.Path, parsed.Dst.Path, progress)
		}
	case transfer.Download:
		if recursive {
			result, err = engine.DownloadRecursive(ctx, parsed.Src.Path, parsed.Dst.Path, progress)
		} else {
			result, err = engine.Download(ctx, parsed.Src.Path, parsed.Dst.Path, progress)
		}
	}

	if err != nil {
		if ctx.Err() != nil {
			return output.Errorf("transfer cancelled", "remote temp file cleaned up")
		}
		return output.Errorf(err.Error(), "")
	}

	renderCpResult(w, result)
	return nil
}

func cpRelayDirect(ctx context.Context, w *output.Writer, store *config.Store, parsed transfer.ParsedArgs, recursive bool, progress transfer.ProgressFunc) error {
	srcHost, err := store.Get(parsed.Src.Alias)
	if err != nil {
		return output.Errorf(err.Error(), "run 'sshq ls' to see available hosts")
	}
	dstHost, err := store.Get(parsed.Dst.Alias)
	if err != nil {
		return output.Errorf(err.Error(), "run 'sshq ls' to see available hosts")
	}

	srcCfg := sshclient.ConnConfig{
		Host: srcHost.HostName, Port: srcHost.Port,
		User: srcHost.User, IdentityFile: srcHost.IdentityFile,
		Timeout: 30 * time.Second,
	}
	dstCfg := sshclient.ConnConfig{
		Host: dstHost.HostName, Port: dstHost.Port,
		User: dstHost.User, IdentityFile: dstHost.IdentityFile,
		Timeout: 30 * time.Second,
	}

	w.Info("connecting to " + parsed.Src.Alias + "...")
	srcClient, err := sshclient.Dial(ctx, srcCfg)
	if err != nil {
		return connErrorToOutput(err, parsed.Src.Alias)
	}
	defer srcClient.Close()

	w.Info("connecting to " + parsed.Dst.Alias + "...")
	dstClient, err := sshclient.Dial(ctx, dstCfg)
	if err != nil {
		return connErrorToOutput(err, parsed.Dst.Alias)
	}
	defer dstClient.Close()

	cache := profileCacheFrom(ctx)
	srcProfile, _ := remote.GetProfile(ctx, srcClient, cache, srcHost.HostName, srcHost.Port)
	dstProfile, _ := remote.GetProfile(ctx, dstClient, cache, dstHost.HostName, dstHost.Port)

	infoFn := func(msg string) { w.Info(msg) }

	var result *transfer.Result
	if recursive {
		result, err = transfer.RunRelayRecursive(ctx, srcClient, dstClient, parsed.Src.Path, parsed.Dst.Path, srcProfile, dstProfile, infoFn, progress)
	} else {
		result, err = transfer.RunRelay(ctx, srcClient, dstClient, parsed.Src.Path, parsed.Dst.Path, srcProfile, dstProfile, infoFn, progress)
	}

	if err != nil {
		if ctx.Err() != nil {
			return output.Errorf("relay cancelled", "remote temp files cleaned up")
		}
		return output.Errorf(err.Error(), "")
	}

	renderCpResult(w, result)
	return nil
}

func renderCpResult(w *output.Writer, r *transfer.Result) {
	if w.IsJSONMode() {
		w.JSONOut(r)
		return
	}
	if r.Files > 1 {
		w.Value(fmt.Sprintf("%s %d files %s %s %s",
			r.Remote, r.Files, transfer.HumanSize(r.Size), r.Duration, r.Engine))
	} else {
		w.Value(fmt.Sprintf("%s %s %s %s",
			r.Remote, transfer.HumanSize(r.Size), r.Duration, r.Engine))
	}
}

func makeProgressFunc(w *output.Writer) transfer.ProgressFunc {
	return func(info transfer.ProgressInfo) {
		if w.IsJSONMode() {
			b, _ := json.Marshal(info)
			w.Info(string(b))
			return
		}
		w.Info(fmt.Sprintf("%s %d%% %s/%s %s",
			info.File, info.Percent,
			transfer.HumanSize(info.Transferred),
			transfer.HumanSize(info.Total),
			info.Speed))
	}
}
