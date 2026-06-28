package cli

import (
	"encoding/json"

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
			w.Render(versionInfo{})
			return nil
		},
	}
}

type versionInfo struct{}

func (versionInfo) Pretty() string { return version.String() }

func (versionInfo) MarshalJSON() ([]byte, error) {
	return json.Marshal(version.Map())
}
