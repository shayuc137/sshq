package cli

import (
	"github.com/shayuc137/sshq/internal/config"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/spf13/cobra"
)

func newLsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List configured SSH hosts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store := configFrom(cmd.Context())
			if store == nil {
				return output.Errorf("no SSH config loaded", "check ~/.ssh/config exists")
			}

			w := writerFrom(cmd.Context())
			hosts := store.List()

			if w.IsJSONMode() {
				w.JSONOut(hostsToMaps(hosts))
				return nil
			}

			pretty, _ := cmd.Flags().GetBool("pretty")
			if pretty {
				w.Value(config.RenderListPretty(hosts))
			} else {
				w.Value(config.RenderListCompact(hosts))
			}
			return nil
		},
	}
}

func hostsToMaps(hosts []config.Host) []map[string]any {
	result := make([]map[string]any, len(hosts))
	for i, h := range hosts {
		m := map[string]any{
			"alias":    h.Alias,
			"hostname": h.HostName,
			"user":     h.User,
			"port":     h.Port,
		}
		if h.IdentityFile != "" {
			m["identity_file"] = h.IdentityFile
		}
		if h.ProxyJump != "" {
			m["proxy_jump"] = h.ProxyJump
		}
		if len(h.Metadata) > 0 {
			m["metadata"] = h.Metadata
		}
		result[i] = m
	}
	return result
}
