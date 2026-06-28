package cli

import (
	"time"

	"github.com/shayuc137/sshq/internal/config"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/remote"
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
			prettyFlag, _ := cmd.Flags().GetBool("pretty")
			noProgress, _ := cmd.Flags().GetBool("no-progress")
			verbose, _ := cmd.Flags().GetBool("verbose")

			var opts []output.Option
			if jsonFlag {
				opts = append(opts, output.WithJSON())
			}
			if prettyFlag {
				opts = append(opts, output.WithPretty())
			}
			if noProgress {
				opts = append(opts, output.WithNoProgress())
			}
			if verbose {
				opts = append(opts, output.WithVerbose())
			}

			w := output.New(cmd.OutOrStdout(), cmd.ErrOrStderr(), opts...)
			ctx := withWriter(cmd.Context(), w)

			cfgPath, _ := cmd.Flags().GetString("config")
			store, err := config.LoadDefault(cfgPath)
			if err != nil {
				w.Info("warning: " + err.Error())
			}
			if store != nil {
				ctx = withConfig(ctx, store)
			}

			cache, _ := remote.NewCache(remote.DefaultTTL)
			if cache != nil {
				ctx = withProfileCache(ctx, cache)
			}

			cmd.SetContext(ctx)
			return nil
		},
	}

	cmd.PersistentFlags().Bool("json", false, "output in JSON format")
	cmd.PersistentFlags().Bool("pretty", false, "human-readable output")
	cmd.PersistentFlags().Bool("no-progress", false, "disable progress output")
	cmd.PersistentFlags().String("config", "", "SSH config file path")
	cmd.PersistentFlags().BoolP("verbose", "v", false, "verbose output")
	cmd.PersistentFlags().Duration("timeout", 30*time.Second, "operation timeout")

	cmd.AddCommand(
		newVersionCommand(),
		newLsCommand(),
		newSearchCommand(),
		newInfoCommand(),
		newExecCommand(),
		newCpCommand(),
		newProbeCommand(),
		newDaemonCommand(),
		newTrustCommand(),
		newConfigCommand(),
		newClusterCommand(),
		newTunnelCommand(),
		newDocsCommand(),
	)

	return cmd
}
