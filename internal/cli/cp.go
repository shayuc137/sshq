package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/shayuc137/sshq/internal/audit"
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
				return output.Errorf(err.Error(), "usage: sshq cp <src> <dst>").WithCode(output.CodeInvalidUsage)
			}

			store := configFrom(cmd.Context())
			if store == nil {
				return output.Errorf("no SSH config loaded", "check ~/.ssh/config exists").WithCode(output.CodeConfigUnavailable)
			}

			w := writerFrom(cmd.Context())
			recursive, _ := cmd.Flags().GetBool("recursive")
			mkdirs, _ := cmd.Flags().GetBool("mkdirs")
			noDaemon, _ := cmd.Flags().GetBool("no-daemon")
			// Copy duration scales with payload size, so the root 30s default is
			// the wrong unit and cp does not inherit it. On the direct path a
			// wedged transfer is still unbounded — a blocked Read never reaches
			// the loop's ctx check, and the SSH channel has no deadline. The
			// daemon path is bounded by recvTransferFrames instead.
			var timeout time.Duration
			if cmd.Flags().Changed("timeout") {
				timeout, _ = cmd.Flags().GetDuration("timeout")
			}
			// The IPC payload carries whole seconds, so a sub-second deadline would
			// truncate to zero — which the daemon reads as "no limit". Round up so
			// an explicit --timeout never silently becomes unlimited on one path
			// while the direct path honors it.
			timeoutSeconds := 0
			if timeout > 0 {
				timeoutSeconds = int((timeout + time.Second - 1) / time.Second)
			}

			// The daemon enforces the same deadline and reports it precisely, so
			// this side waits a little longer and only speaks up when that frame
			// never arrives.
			var clientDeadline time.Time
			if timeout > 0 {
				clientDeadline = time.Now().Add(timeout + daemonDeadlineGrace)
			}

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
					env, _ := ipc.MakeEnvelope("transfer", transferPayload(parsed, recursive, mkdirs, w.IsVerbose(), timeoutSeconds))
					return daemonDispatch(env,
						func(conn net.Conn) error {
							return recvTransferFrames(w, conn, clientDeadline)
						},
						func(reason string) error {
							w.Info(reason + ", falling back to direct connection")
							return cpTransferDirect(ctx, w, store, parsed, recursive, mkdirs, progressFn)
						},
					)
				case transfer.Relay:
					env, _ := ipc.MakeEnvelope("relay", ipc.RelayPayload{
						SrcAlias:  parsed.Src.Alias,
						SrcPath:   parsed.Src.Path,
						DstAlias:  parsed.Dst.Alias,
						DstPath:   parsed.Dst.Path,
						Timeout:   timeoutSeconds,
						Recursive: recursive,
						Mkdirs:    mkdirs,
						Verbose:   w.IsVerbose(),
					})
					return daemonDispatch(env,
						func(conn net.Conn) error {
							return recvTransferFrames(w, conn, clientDeadline)
						},
						func(reason string) error {
							w.Info(reason + ", falling back to direct connection")
							return cpRelayDirect(ctx, w, store, parsed, recursive, mkdirs, progressFn)
						},
					)
				}
			}

			switch parsed.Direction {
			case transfer.Upload, transfer.Download:
				return cpTransferDirect(ctx, w, store, parsed, recursive, mkdirs, progressFn)
			case transfer.Relay:
				return cpRelayDirect(ctx, w, store, parsed, recursive, mkdirs, progressFn)
			}
			return nil
		},
	}

	cmd.Flags().BoolP("recursive", "r", false, "copy directories recursively")
	cmd.Flags().Bool("mkdirs", false, "create missing remote destination parent directories")
	cmd.Flags().Bool("no-daemon", false, "skip daemon, connect directly")
	return cmd
}

// --- daemon paths ---

func transferPayload(parsed transfer.ParsedArgs, recursive, mkdirs, verbose bool, timeout int) ipc.TransferPayload {
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
		Timeout:    timeout,
		Recursive:  recursive,
		Mkdirs:     mkdirs,
		Verbose:    verbose,
	}
}

// frameStallTimeout bounds the wait for any daemon frame. It has to clear the
// pre-transfer phase — connect, shell detection, SFTP negotiation — which the
// daemon itself budgets 30s for, so it sits well above that. Progress frames
// are throttled to 5s once bytes are moving, far below this.
var frameStallTimeout = 60 * time.Second

// daemonDeadlineGrace keeps the daemon's own timeout frame ahead of this side's
// fallback. Both clocks would otherwise race, and the daemon's message is the
// better one — it knows the transfer stopped and the temp file was removed.
const daemonDeadlineGrace = 5 * time.Second

// recvTransferFrames reads daemon frames until the transfer resolves, giving up
// if the daemon goes quiet. overall bounds the whole exchange when the caller
// passed an explicit --timeout; the zero value means no overall bound.
//
// The lever here is the IPC socket's read deadline, which unix sockets support
// on every platform sshq targets. That is what makes this fix possible at all —
// the transfer's own SSH channel has no deadline, which is why a wedged copy
// cannot be interrupted on the daemon side (see the dev-guide gotcha).
func recvTransferFrames(w *output.Writer, conn net.Conn, overall time.Time) error {
	for {
		deadline := time.Now().Add(frameStallTimeout)
		if !overall.IsZero() && overall.Before(deadline) {
			deadline = overall
		}
		conn.SetReadDeadline(deadline)

		msg, err := ipc.Recv(conn)
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				// The daemon may still be transferring, or still wedged. Unlike
				// the direct path this side cannot promise the remote temp file
				// was cleaned up, so the hint must not claim it was.
				return output.Errorf(
					"daemon stopped responding; the transfer may still be running and the remote temporary file may remain",
					"check remote state with sshq exec before retrying",
				).WithCode(output.CodeResultIndeterminate)
			}
			return output.Errorf("daemon connection lost", "retry or use --no-daemon").WithCode(output.CodeResultIndeterminate)
		}

		var frame ipc.Frame
		if err := json.Unmarshal(msg, &frame); err != nil {
			return output.Errorf("invalid daemon response", "").WithCode(output.CodeDaemonError)
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
			return output.Errorf(frame.Hint, frame.Action).WithCode(output.CodeOrInternal(frame.ErrorCode()))
		}
	}
}

// --- direct paths (fallback) ---

func cpTransferDirect(ctx context.Context, w *output.Writer, store *config.Store, parsed transfer.ParsedArgs, recursive, mkdirs bool, progress transfer.ProgressFunc) error {
	if err := checkPolicyTransfer(ctx, parsed); err != nil {
		return err
	}

	alias, direction, localPath, remotePath := transferAuditParts(parsed)
	auditStart := time.Now()

	host, err := store.Get(alias)
	if err != nil {
		entry := audit.TransferEntry(alias, direction, localPath, remotePath, audit.ResultError, time.Since(auditStart).Milliseconds(), audit.SourceDirect)
		if auditErr := recordAuditError(ctx, entry, err); auditErr != nil {
			return auditErr
		}
		return output.Errorf(err.Error(), "run 'sshq ls' to see available hosts").WithCode(output.CodeHostNotFound)
	}

	cfg, err := hostToConnConfigWithCredentials(host, store, credentialStoreFrom(ctx))
	if err != nil {
		entry := audit.TransferEntry(alias, direction, localPath, remotePath, audit.ResultError, time.Since(auditStart).Milliseconds(), audit.SourceDirect)
		if auditErr := recordAuditError(ctx, entry, err); auditErr != nil {
			return auditErr
		}
		return credentialOutputError(err, alias)
	}
	cfg.Timeout = 30 * time.Second

	w.Verbose("connecting to " + alias + "...")
	connectStart := time.Now()
	client, err := sshclient.Dial(ctx, cfg)
	if err != nil {
		entry := audit.TransferEntry(alias, direction, localPath, remotePath, audit.ResultError, time.Since(auditStart).Milliseconds(), audit.SourceDirect)
		if auditErr := recordAuditError(ctx, entry, err); auditErr != nil {
			return auditErr
		}
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

	engine, err := transfer.NewEngine(client, profile, func(msg string) { w.Info(msg) }, transferEngineOptions(mkdirs)...)
	if err != nil {
		entry := audit.TransferEntry(alias, direction, localPath, remotePath, audit.ResultError, time.Since(auditStart).Milliseconds(), audit.SourceDirect)
		if auditErr := recordAuditError(ctx, entry, err); auditErr != nil {
			return auditErr
		}
		return output.Errorf("transfer engine: "+err.Error(), "").WithCode(output.CodeTransferFailed)
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
		entry := audit.TransferEntry(alias, direction, localPath, remotePath, audit.ResultError, time.Since(auditStart).Milliseconds(), audit.SourceDirect)
		if auditErr := recordAuditError(ctx, entry, err); auditErr != nil {
			return auditErr
		}
		return cpErrorToOutput(ctx, err, parsed, recursive)
	}

	if err := recordAudit(ctx, audit.TransferEntry(alias, direction, localPath, remotePath, audit.ResultSuccess, time.Since(auditStart).Milliseconds(), audit.SourceDirect)); err != nil {
		return err
	}
	w.Render(result)
	return nil
}

func cpRelayDirect(ctx context.Context, w *output.Writer, store *config.Store, parsed transfer.ParsedArgs, recursive, mkdirs bool, progress transfer.ProgressFunc) error {
	if err := checkPolicyTransfer(ctx, parsed); err != nil {
		return err
	}

	auditStart := time.Now()
	srcHost, err := store.Get(parsed.Src.Alias)
	if err != nil {
		entry := audit.RelayEntry(parsed.Src.Alias, parsed.Src.Path, parsed.Dst.Alias, parsed.Dst.Path, audit.ResultError, time.Since(auditStart).Milliseconds(), audit.SourceDirect)
		if auditErr := recordAuditError(ctx, entry, err); auditErr != nil {
			return auditErr
		}
		return output.Errorf(err.Error(), "run 'sshq ls' to see available hosts").WithCode(output.CodeHostNotFound)
	}
	dstHost, err := store.Get(parsed.Dst.Alias)
	if err != nil {
		entry := audit.RelayEntry(parsed.Src.Alias, parsed.Src.Path, parsed.Dst.Alias, parsed.Dst.Path, audit.ResultError, time.Since(auditStart).Milliseconds(), audit.SourceDirect)
		if auditErr := recordAuditError(ctx, entry, err); auditErr != nil {
			return auditErr
		}
		return output.Errorf(err.Error(), "run 'sshq ls' to see available hosts").WithCode(output.CodeHostNotFound)
	}

	srcCfg, err := hostToConnConfigWithCredentials(srcHost, store, credentialStoreFrom(ctx))
	if err != nil {
		entry := audit.RelayEntry(parsed.Src.Alias, parsed.Src.Path, parsed.Dst.Alias, parsed.Dst.Path, audit.ResultError, time.Since(auditStart).Milliseconds(), audit.SourceDirect)
		if auditErr := recordAuditError(ctx, entry, err); auditErr != nil {
			return auditErr
		}
		return credentialOutputError(err, parsed.Src.Alias)
	}
	srcCfg.Timeout = 30 * time.Second
	dstCfg, err := hostToConnConfigWithCredentials(dstHost, store, credentialStoreFrom(ctx))
	if err != nil {
		entry := audit.RelayEntry(parsed.Src.Alias, parsed.Src.Path, parsed.Dst.Alias, parsed.Dst.Path, audit.ResultError, time.Since(auditStart).Milliseconds(), audit.SourceDirect)
		if auditErr := recordAuditError(ctx, entry, err); auditErr != nil {
			return auditErr
		}
		return credentialOutputError(err, parsed.Dst.Alias)
	}
	dstCfg.Timeout = 30 * time.Second

	w.Verbose("connecting to " + parsed.Src.Alias + "...")
	srcConnectStart := time.Now()
	srcClient, err := sshclient.Dial(ctx, srcCfg)
	if err != nil {
		entry := audit.RelayEntry(parsed.Src.Alias, parsed.Src.Path, parsed.Dst.Alias, parsed.Dst.Path, audit.ResultError, time.Since(auditStart).Milliseconds(), audit.SourceDirect)
		if auditErr := recordAuditError(ctx, entry, err); auditErr != nil {
			return auditErr
		}
		return connErrorToOutput(err, parsed.Src.Alias)
	}
	defer srcClient.Close()
	w.Verbose("connection: alias=" + parsed.Src.Alias + " duration=" + verboseDuration(time.Since(srcConnectStart)) + " direct")

	w.Verbose("connecting to " + parsed.Dst.Alias + "...")
	dstConnectStart := time.Now()
	dstClient, err := sshclient.Dial(ctx, dstCfg)
	if err != nil {
		entry := audit.RelayEntry(parsed.Src.Alias, parsed.Src.Path, parsed.Dst.Alias, parsed.Dst.Path, audit.ResultError, time.Since(auditStart).Milliseconds(), audit.SourceDirect)
		if auditErr := recordAuditError(ctx, entry, err); auditErr != nil {
			return auditErr
		}
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
		result, err = transfer.RunRelayRecursive(ctx, srcClient, dstClient, parsed.Src.Path, parsed.Dst.Path, srcProfile, dstProfile, infoFn, progress, transferEngineOptions(mkdirs)...)
	} else {
		result, err = transfer.RunRelay(ctx, srcClient, dstClient, parsed.Src.Path, parsed.Dst.Path, srcProfile, dstProfile, infoFn, progress, transferEngineOptions(mkdirs)...)
	}

	if err != nil {
		entry := audit.RelayEntry(parsed.Src.Alias, parsed.Src.Path, parsed.Dst.Alias, parsed.Dst.Path, audit.ResultError, time.Since(auditStart).Milliseconds(), audit.SourceDirect)
		if auditErr := recordAuditError(ctx, entry, err); auditErr != nil {
			return auditErr
		}
		return cpErrorToOutput(ctx, err, parsed, recursive)
	}

	if err := recordAudit(ctx, audit.RelayEntry(parsed.Src.Alias, parsed.Src.Path, parsed.Dst.Alias, parsed.Dst.Path, audit.ResultSuccess, time.Since(auditStart).Milliseconds(), audit.SourceDirect)); err != nil {
		return err
	}
	w.Render(result)
	w.Verbose("transfer engine: " + result.Engine)
	return nil
}

func transferEngineOptions(mkdirs bool) []transfer.EngineOption {
	if mkdirs {
		return []transfer.EngineOption{transfer.WithMkdirs()}
	}
	return nil
}

func cpErrorToOutput(ctx context.Context, err error, parsed transfer.ParsedArgs, recursive bool) *output.CmdError {
	operation := "transfer"
	tempState := "temporary file cleaned up"
	if parsed.Direction == transfer.Relay {
		operation = "relay"
		tempState = "temporary files cleaned up"
	}

	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return output.Errorf(
			fmt.Sprintf("%s deadline exceeded; partial data may have been transferred; %s", operation, tempState),
			"increase --timeout and retry",
		).WithCode(output.CodeTimeout)
	case ctx.Err() != nil:
		return output.Errorf(
			fmt.Sprintf("%s cancelled; %s", operation, tempState),
			"re-run the command to retry",
		).WithCode(output.CodeTransferFailed)
	}

	var missingParent *transfer.RemoteParentMissingError
	if errors.As(err, &missingParent) {
		return output.Errorf(err.Error(), cpMkdirsAction(parsed, recursive)).WithCode(output.CodeTransferFailed)
	}
	return output.Errorf(err.Error(), "").WithCode(output.CodeTransferFailed)
}

func contextWithTransferTimeout(timeoutSeconds int) (context.Context, context.CancelFunc) {
	if timeoutSeconds <= 0 {
		return context.Background(), func() {}
	}
	return context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
}

func cpMkdirsAction(parsed transfer.ParsedArgs, recursive bool) string {
	args := []string{"sshq", "cp", "--mkdirs"}
	if recursive {
		args = append(args, "--recursive")
	}
	args = append(args, formatTransferEndpoint(parsed.Src), formatTransferEndpoint(parsed.Dst))
	for i := range args {
		args[i] = quoteCommandArg(args[i])
	}
	return strings.Join(args, " ")
}

func formatTransferEndpoint(endpoint transfer.Endpoint) string {
	if endpoint.Alias == "" {
		return endpoint.Path
	}
	return endpoint.Alias + ":" + endpoint.Path
}

func parsedArgsFromTransferPayload(payload ipc.TransferPayload) transfer.ParsedArgs {
	local := transfer.Endpoint{Path: payload.LocalPath}
	remoteEndpoint := transfer.Endpoint{Alias: payload.Alias, Path: payload.RemotePath}
	if payload.Direction == "download" {
		return transfer.ParsedArgs{Direction: transfer.Download, Src: remoteEndpoint, Dst: local}
	}
	return transfer.ParsedArgs{Direction: transfer.Upload, Src: local, Dst: remoteEndpoint}
}

func parsedArgsFromRelayPayload(payload ipc.RelayPayload) transfer.ParsedArgs {
	return transfer.ParsedArgs{
		Direction: transfer.Relay,
		Src:       transfer.Endpoint{Alias: payload.SrcAlias, Path: payload.SrcPath},
		Dst:       transfer.Endpoint{Alias: payload.DstAlias, Path: payload.DstPath},
	}
}

func quoteCommandArg(arg string) string {
	if arg != "" && strings.IndexFunc(arg, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-._/:~", r))
	}) == -1 {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
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
