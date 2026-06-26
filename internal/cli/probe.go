package cli

import (
	"sort"
	"strings"
	"time"

	"github.com/shayuc137/sshq/internal/config"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/probe"
	"github.com/spf13/cobra"
)

func newProbeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "probe <alias>",
		Short: "Check TCP connectivity to a host",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store := configFrom(cmd.Context())
			if store == nil {
				return output.Errorf("no SSH config loaded", "check ~/.ssh/config exists")
			}

			w := writerFrom(cmd.Context())
			timeout, _ := cmd.Flags().GetDuration("timeout")
			portOverride, _ := cmd.Flags().GetString("port")
			all, _ := cmd.Flags().GetBool("all")
			pretty, _ := cmd.Flags().GetBool("pretty")

			if all {
				return runProbeAll(cmd, store, w, timeout, portOverride, pretty)
			}

			if len(args) == 0 {
				return output.Errorf("alias required", "use 'sshq probe <alias>' or 'sshq probe --all'")
			}

			alias := args[0]
			host, err := store.Get(alias)
			if err != nil {
				return output.Errorf(err.Error(), "run 'sshq ls' to see available hosts")
			}

			port := host.Port
			if portOverride != "" {
				port = portOverride
			}

			r := probe.Check(cmd.Context(), host.HostName, port, timeout)
			r.Alias = alias

			if w.IsJSONMode() {
				w.JSONOut(r)
				return nil
			}
			if pretty {
				w.Value(probe.RenderPretty(r))
			} else {
				w.Value(probe.RenderCompact(r))
			}
			return nil
		},
	}

	cmd.Flags().String("port", "", "override port to probe")
	cmd.Flags().Bool("all", false, "probe all configured hosts")

	return cmd
}

func runProbeAll(cmd *cobra.Command, store *config.Store, w *output.Writer, timeout time.Duration, portOverride string, pretty bool) error {
	hosts := store.List()
	targets := make([]probe.Target, len(hosts))
	for i, h := range hosts {
		port := h.Port
		if portOverride != "" {
			port = portOverride
		}
		targets[i] = probe.Target{Alias: h.Alias, Host: h.HostName, Port: port}
	}

	results := probe.CheckAll(cmd.Context(), targets, timeout, 10)

	if w.IsJSONMode() {
		w.JSONOut(results)
		return nil
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Alias < results[j].Alias
	})

	var b strings.Builder
	for _, r := range results {
		if pretty {
			b.WriteString(probe.RenderPretty(r) + "\n")
		} else {
			b.WriteString(probe.RenderCompact(r) + "\n")
		}
	}
	b.WriteString(probe.RenderBatchSummary(results) + "\n")
	w.Value(b.String())
	return nil
}
