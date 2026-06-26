package cli

import (
	"github.com/shayuc137/sshq/internal/config"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/spf13/cobra"
)

func newInfoCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "info <alias>",
		Short: "Show detailed host information",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store := configFrom(cmd.Context())
			if store == nil {
				return output.Errorf("no SSH config loaded", "check ~/.ssh/config exists")
			}

			host, err := store.Get(args[0])
			if err != nil {
				return output.Errorf(err.Error(), "run 'sshq ls' to see available hosts")
			}

			w := writerFrom(cmd.Context())

			if w.IsJSONMode() {
				m := hostsToMaps([]config.Host{host})
				w.JSONOut(m[0])
				return nil
			}

			pretty, _ := cmd.Flags().GetBool("pretty")
			if pretty {
				w.Value(config.RenderInfoPretty(host))
			} else {
				w.Value(config.RenderInfoCompact(host))
			}
			return nil
		},
	}
}
