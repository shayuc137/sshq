package cli

import (
	"time"

	"github.com/shayuc137/sshq/internal/output"
	"github.com/spf13/cobra"
)

func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "sshq",
		Short:         "Agent-native SSH multiplexing CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			jsonFlag, _ := cmd.Flags().GetBool("json")
			if jsonFlag || output.DetectEnvJSONMode() {
				w := output.New(cmd.OutOrStdout(), cmd.ErrOrStderr())
				w.SetJSONMode(true)
				cmd.SetContext(withWriter(cmd.Context(), w))
			} else {
				w := output.New(cmd.OutOrStdout(), cmd.ErrOrStderr())
				cmd.SetContext(withWriter(cmd.Context(), w))
			}
			return nil
		},
	}

	cmd.PersistentFlags().Bool("json", false, "output in JSON format")
	cmd.PersistentFlags().BoolP("verbose", "v", false, "verbose output")
	cmd.PersistentFlags().Duration("timeout", 30*time.Second, "operation timeout")

	cmd.AddCommand(
		newVersionCommand(),
		newStubCommand("exec", "Execute a command on a remote host", 1),
		newStubCommand("cp", "Copy files between local and remote hosts", 2),
		newStubCommand("ls", "List configured SSH hosts", 1),
		newStubCommand("search", "Search SSH hosts by pattern", 1),
		newStubCommand("info", "Show detailed host information", 1),
		newStubCommand("probe", "Check TCP connectivity to a host", 1),
		newStubCommand("daemon", "Manage the connection pool daemon", 1),
		newStubCommand("config", "Manage sshq configuration", 1),
	)

	return cmd
}
