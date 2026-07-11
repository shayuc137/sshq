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
				return output.Errorf("no SSH config loaded", "check ~/.ssh/config exists").WithCode(output.CodeConfigUnavailable)
			}

			w := writerFrom(cmd.Context())
			w.Render(config.HostList(store.List()))
			return nil
		},
	}
}
