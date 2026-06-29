package cli

import (
	"fmt"
	"strings"

	auditpkg "github.com/shayuc137/sshq/internal/audit"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/spf13/cobra"
)

func newAuditCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Query structured audit logs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := writerFrom(cmd.Context())
			last, _ := cmd.Flags().GetInt("last")
			if last < 0 {
				return output.Errorf("--last must be non-negative", "use --last 50")
			}
			alias, _ := cmd.Flags().GetString("alias")
			operation, _ := cmd.Flags().GetString("operation")

			path := ""
			if cfg := appConfigFrom(cmd.Context()); cfg != nil {
				path = cfg.Audit.Path
			}
			opts := auditpkg.QueryOpts{
				Last:      last,
				Alias:     alias,
				Operation: operation,
			}
			if w.IsVerbose() {
				opts.Warn = func(msg string) { w.Verbose(msg) }
			}
			entries, err := auditpkg.Query(path, opts)
			if err != nil {
				return output.Errorf("read audit log: "+err.Error(), "check [audit] path and permissions")
			}
			w.Render(auditList(entries))
			return nil
		},
	}
	cmd.Flags().Int("last", 50, "show the last N audit entries")
	cmd.Flags().String("alias", "", "filter audit entries by host alias")
	cmd.Flags().String("operation", "", "filter audit entries by operation")
	return cmd
}

type auditList []auditpkg.Entry

func (l auditList) Pretty() string {
	if len(l) == 0 {
		return "no audit entries"
	}
	var b strings.Builder
	for i, entry := range l {
		if i > 0 {
			b.WriteByte('\n')
		}
		target := entry.Alias
		if target == "" && len(entry.Aliases) > 0 {
			target = strings.Join(entry.Aliases, ",")
		}
		fmt.Fprintf(&b, "%s %s %s %s %s duration=%dms",
			entry.Timestamp, entry.Source, entry.Operation, target, entry.Result, entry.DurationMs)
		if entry.ExitCode != nil {
			fmt.Fprintf(&b, " exit=%d", *entry.ExitCode)
		}
		if entry.BlockedBy != "" {
			fmt.Fprintf(&b, " blocked_by=%s", entry.BlockedBy)
		}
		if entry.MatchedPattern != "" {
			fmt.Fprintf(&b, " pattern=%q", entry.MatchedPattern)
		}
		if entry.ErrorHint != "" {
			fmt.Fprintf(&b, " error=%q", entry.ErrorHint)
		}
		if entry.Summary != "" {
			fmt.Fprintf(&b, " summary=%q", entry.Summary)
		}
	}
	return b.String()
}
