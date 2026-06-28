package cli

import (
	"context"
	"encoding/json"
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
			noDaemon, _ := cmd.Flags().GetBool("no-daemon")
			timeout, _ := cmd.Flags().GetDuration("timeout")

			progressFn := transferProgress(w)

			ctx := cmd.Context()
			if timeout > 0 {
				var cancel func()
				ctx, cancel = context.WithTimeout(ctx, timeout)
				defer cancel()
			}

			if !noDaemon && ipc.IsRunning() {
				switch parsed.Direction {
				case transfer.Upload, transfer.Download:
					env, _ := ipc.MakeEnvelope("transfer", transferPayload(parsed, recursive, w.IsVerbose()))
					return daemonDispatch(env,
						func(conn net.Conn) error {
							return recvTransferFrames(w, conn)
						},
						func(reason string) error {
							w.Info(reason + ", falling back to direct connection")
							return cpTransferDirect(ctx, w, store, parsed, recursive, progressFn)
						},
					)
				case transfer.Relay:
					env, _ := ipc.MakeEnvelope("relay", ipc.RelayPayload{
						SrcAlias:  parsed.Src.Alias,
						SrcPath:   parsed.Src.Path,
						DstAlias:  parsed.Dst.Alias,
						DstPath:   parsed.Dst.Path,
						Recursive: recursive,
						Verbose:   w.IsVerbose(),
					})
					return daemonDispatch(env,
						func(conn net.Conn) error {
							return recvTransferFrames(w, conn)
						},
						func(reason string) error {
							w.Info(reason + ", falling back to direct connection")
							return cpRelayDirect(ctx, w, store, parsed, recursive, progressFn)
						},
					)
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
	cmd.Flags().Bool("no-daemon", false, "skip daemon, connect directly")
	return cmd
}

// --- daemon paths ---

func transferPayload(parsed transfer.ParsedArgs, recursive, verbose bool) ipc.TransferPayload {
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

	return ipc.TransferPayload{
		Direction:  direction,
		Alias:      alias,
		LocalPath:  localPath,
		RemotePath: remotePath,
		Recursive:  recursive,
		Verbose:    verbose,
	}
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
		case daemonVerboseFrame:
			recvVerboseFrame(w, frame)
		case "stderr":
			w.Info(frame.Data)
		case "progress":
			var info transfer.ProgressInfo
			json.Unmarshal(frame.Payload, &info)
			w.Progress(output.ProgressInfo{
				File:        info.File,
				Percent:     info.Percent,
				Transferred: info.Transferred,
				Total:       info.Total,
				Speed:       info.Speed,
			})
		case "result":
			var result transfer.Result
			json.Unmarshal(frame.Payload, &result)
			w.Verbose("transfer engine: " + result.Engine)
			w.Render(&result)
			return nil
		case "error":
			return output.Errorf(frame.Hint, frame.Action)
		}
	}
}

// --- direct paths (fallback) ---

func cpTransferDirect(ctx context.Context, w *output.Writer, store *config.Store, parsed transfer.ParsedArgs, recursive bool, progress transfer.ProgressFunc) error {
	alias := parsed.Src.Alias
	if alias == "" {
		alias = parsed.Dst.Alias
	}

	host, err := store.Get(alias)
	if err != nil {
		return output.Errorf(err.Error(), "run 'sshq ls' to see available hosts")
	}

	cfg := hostToConnConfigWithStore(host, store)
	cfg.Timeout = 30 * time.Second

	w.Info("connecting to " + alias + "...")
	connectStart := time.Now()
	client, err := sshclient.Dial(ctx, cfg)
	if err != nil {
		return connErrorToOutput(err, alias)
	}
	defer client.Close()
	w.Verbose("connection: alias=" + alias + " duration=" + verboseDuration(time.Since(connectStart)) + " direct")

	cache := profileCacheFrom(ctx)
	profile, profileErr := remote.GetProfile(ctx, client, cache, host.HostName, host.Port)
	if profileErr != nil {
		w.Verbose("shell detection warning: " + profileErr.Error())
	}
	w.Verbose(verboseProfile(profile))

	engine, err := transfer.NewEngine(client, profile, func(msg string) { w.Info(msg) })
	if err != nil {
		return output.Errorf("transfer engine: "+err.Error(), "")
	}
	defer engine.Close()
	w.Verbose("transfer engine: " + engine.Name())

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

	w.Render(result)
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

	srcCfg := hostToConnConfigWithStore(srcHost, store)
	srcCfg.Timeout = 30 * time.Second
	dstCfg := hostToConnConfigWithStore(dstHost, store)
	dstCfg.Timeout = 30 * time.Second

	w.Info("connecting to " + parsed.Src.Alias + "...")
	srcConnectStart := time.Now()
	srcClient, err := sshclient.Dial(ctx, srcCfg)
	if err != nil {
		return connErrorToOutput(err, parsed.Src.Alias)
	}
	defer srcClient.Close()
	w.Verbose("connection: alias=" + parsed.Src.Alias + " duration=" + verboseDuration(time.Since(srcConnectStart)) + " direct")

	w.Info("connecting to " + parsed.Dst.Alias + "...")
	dstConnectStart := time.Now()
	dstClient, err := sshclient.Dial(ctx, dstCfg)
	if err != nil {
		return connErrorToOutput(err, parsed.Dst.Alias)
	}
	defer dstClient.Close()
	w.Verbose("connection: alias=" + parsed.Dst.Alias + " duration=" + verboseDuration(time.Since(dstConnectStart)) + " direct")

	cache := profileCacheFrom(ctx)
	srcProfile, srcProfileErr := remote.GetProfile(ctx, srcClient, cache, srcHost.HostName, srcHost.Port)
	if srcProfileErr != nil {
		w.Verbose("source shell detection warning: " + srcProfileErr.Error())
	}
	w.Verbose("source " + verboseProfile(srcProfile))
	dstProfile, dstProfileErr := remote.GetProfile(ctx, dstClient, cache, dstHost.HostName, dstHost.Port)
	if dstProfileErr != nil {
		w.Verbose("destination shell detection warning: " + dstProfileErr.Error())
	}
	w.Verbose("destination " + verboseProfile(dstProfile))

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

	w.Render(result)
	w.Verbose("transfer engine: " + result.Engine)
	return nil
}

func transferProgress(w *output.Writer) transfer.ProgressFunc {
	return func(i transfer.ProgressInfo) {
		w.Progress(output.ProgressInfo{
			File:        i.File,
			Percent:     i.Percent,
			Transferred: i.Transferred,
			Total:       i.Total,
			Speed:       i.Speed,
		})
	}
}
