package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/shayuc137/sshq/internal/config"
	"github.com/shayuc137/sshq/internal/output"
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

			switch parsed.Direction {
			case transfer.Upload:
				return cpTransfer(ctx, w, store, parsed, recursive, progressFn)
			case transfer.Download:
				return cpTransfer(ctx, w, store, parsed, recursive, progressFn)
			case transfer.Relay:
				return output.Errorf("server-to-server relay not yet implemented", "planned for Phase 2 C3")
			}
			return nil
		},
	}

	cmd.Flags().BoolP("recursive", "r", false, "copy directories recursively")
	cmd.Flags().Bool("no-progress", false, "disable progress output")
	return cmd
}

func cpTransfer(ctx context.Context, w *output.Writer, store *config.Store, parsed transfer.ParsedArgs, recursive bool, progress transfer.ProgressFunc) error {
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

	engine, err := transfer.NewEngine(client, func(msg string) { w.Info(msg) })
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
