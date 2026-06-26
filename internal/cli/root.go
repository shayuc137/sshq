package cli

import (
	"time"

	"github.com/shayuc137/sshq/internal/config"
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
			w := output.New(cmd.OutOrStdout(), cmd.ErrOrStderr())
			jsonFlag, _ := cmd.Flags().GetBool("json")
			if jsonFlag || output.DetectEnvJSONMode() {
				w.SetJSONMode(true)
			}
			ctx := withWriter(cmd.Context(), w)

			cfgPath, _ := cmd.Flags().GetString("config")
			store, err := config.LoadDefault(cfgPath)
			if err != nil {
				w.Info("warning: " + err.Error())
			}
			if store != nil {
				ctx = withConfig(ctx, store)
			}

			cmd.SetContext(ctx)
			return nil
		},
	}

	cmd.PersistentFlags().Bool("json", false, "output in JSON format")
	cmd.PersistentFlags().Bool("pretty", false, "human-readable output")
	cmd.PersistentFlags().String("config", "", "SSH config file path")
	cmd.PersistentFlags().BoolP("verbose", "v", false, "verbose output")
	cmd.PersistentFlags().Duration("timeout", 30*time.Second, "operation timeout")

	cmd.AddCommand(
		newVersionCommand(),
		newLsCommand(),
		newSearchCommand(),
		newInfoCommand(),
		newExecCommand(),
		newStubCommand("cp", "Copy files between local and remote hosts", 2),
		newStubCommand("probe", "Check TCP connectivity to a host", 1),
		newStubCommand("daemon", "Manage the connection pool daemon", 1),
		newStubCommand("config", "Manage sshq configuration", 1),
	)

	return cmd
}
