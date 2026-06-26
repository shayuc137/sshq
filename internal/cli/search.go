package cli

import (
	"github.com/shayuc137/sshq/internal/config"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/spf13/cobra"
)

func newSearchCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "search <pattern>",
		Short: "Search SSH hosts by pattern",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store := configFrom(cmd.Context())
			if store == nil {
				return output.Errorf("no SSH config loaded", "check ~/.ssh/config exists")
			}

			w := writerFrom(cmd.Context())
			hosts := store.Search(args[0])

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
