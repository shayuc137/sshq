package cli

import (
	"github.com/shayuc137/sshq/internal/version"
	"github.com/spf13/cobra"
)

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := writerFrom(cmd.Context())
			if w.IsJSONMode() {
				w.JSONOut(version.Map())
				return nil
			}
			w.Value(version.String())
			return nil
		},
	}
}
