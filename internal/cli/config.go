package cli

import (
	"fmt"
	"strings"

	"github.com/shayuc137/sshq/internal/config"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/spf13/cobra"
)

func newConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage SSH host configuration",
	}
	cmd.AddCommand(
		newConfigAddCommand(),
		newConfigSetCommand(),
		newConfigRemoveCommand(),
		newConfigListCommand(),
	)
	return cmd
}

func newConfigAddCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <alias>",
		Short: "Add a new SSH host",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store := configFrom(cmd.Context())
			if store == nil {
				return output.Errorf("no SSH config loaded", "check ~/.ssh/config exists").WithCode(output.CodeConfigUnavailable)
			}
			w := writerFrom(cmd.Context())
			alias := args[0]

			hostname, _ := cmd.Flags().GetString("hostname")
			if hostname == "" {
				return output.Errorf("--hostname is required", "usage: sshq config add <alias> --hostname <host>").WithCode(output.CodeInvalidUsage)
			}

			h := config.Host{
				Alias:    alias,
				HostName: hostname,
				Metadata: make(map[string]string),
			}
			if v, _ := cmd.Flags().GetString("user"); v != "" {
				h.User = v
			}
			if v, _ := cmd.Flags().GetString("port"); v != "" {
				h.Port = v
			}
			if v, _ := cmd.Flags().GetString("identity"); v != "" {
				h.IdentityFile = v
			}
			if v, _ := cmd.Flags().GetString("proxy-jump"); v != "" {
				h.ProxyJump = v
			}
			if v, _ := cmd.Flags().GetString("tag"); v != "" {
				h.Metadata["tags"] = v
			}
			if v, _ := cmd.Flags().GetString("env"); v != "" {
				h.Metadata["environment"] = v
			}
			if v, _ := cmd.Flags().GetString("desc"); v != "" {
				h.Metadata["description"] = v
			}

			if err := store.Add(h); err != nil {
				return output.Errorf(err.Error(), "").WithCode(output.CodeInvalidUsage)
			}
			if err := store.Save(); err != nil {
				return output.Errorf("save failed: "+err.Error(), "").WithCode(output.CodeConfigUnavailable)
			}

			return renderConfigVerification(w, store, alias)
		},
	}

	cmd.Flags().String("hostname", "", "remote hostname or IP (required)")
	cmd.Flags().String("user", "", "SSH user")
	cmd.Flags().String("port", "", "SSH port")
	cmd.Flags().String("identity", "", "identity file path")
	cmd.Flags().String("proxy-jump", "", "ProxyJump host")
	cmd.Flags().String("tag", "", "comma-separated tags")
	cmd.Flags().String("env", "", "environment identifier")
	cmd.Flags().String("desc", "", "host description")
	return cmd
}

func newConfigSetCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <alias> <key> <value>",
		Short: "Set a host property or metadata",
		Long: `Set SSH properties (hostname, user, port, identityfile, proxyjump)
or sshq metadata (tags, env, description) on an existing host.

Examples:
  sshq config set myhost hostname 10.0.0.1
  sshq config set myhost tags prod,web
  sshq config set myhost description "production web server"`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			store := configFrom(cmd.Context())
			if store == nil {
				return output.Errorf("no SSH config loaded", "check ~/.ssh/config exists").WithCode(output.CodeConfigUnavailable)
			}
			w := writerFrom(cmd.Context())

			alias, key, value := args[0], args[1], args[2]

			if strings.ToLower(key) == "password" {
				return output.Errorf("passwords must not be stored in ssh config", "use ssh-agent or identity files").WithCode(output.CodeInvalidUsage)
			}

			if err := store.Set(alias, key, value); err != nil {
				return output.Errorf(err.Error(), "").WithCode(output.CodeInvalidUsage)
			}
			if err := store.Save(); err != nil {
				return output.Errorf("save failed: "+err.Error(), "").WithCode(output.CodeConfigUnavailable)
			}

			return renderConfigVerification(w, store, alias)
		},
	}
	return cmd
}

func renderConfigVerification(w *output.Writer, store *config.Store, alias string) error {
	host, err := store.Get(alias)
	if err != nil {
		return output.Errorf("resolve saved host: "+err.Error(), "sshq doctor "+alias).WithCode(output.CodeConfigUnavailable)
	}
	w.Render(config.NewHostVerification(host, "sshq doctor "+alias))
	return nil
}

func newConfigRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "remove <alias>",
		Aliases: []string{"rm"},
		Short:   "Remove a host from SSH config",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store := configFrom(cmd.Context())
			if store == nil {
				return output.Errorf("no SSH config loaded", "check ~/.ssh/config exists").WithCode(output.CodeConfigUnavailable)
			}
			w := writerFrom(cmd.Context())
			alias := args[0]

			if err := store.Remove(alias); err != nil {
				return output.Errorf(err.Error(), "run 'sshq ls' to see available hosts").WithCode(output.CodeHostNotFound)
			}
			if err := store.Save(); err != nil {
				return output.Errorf("save failed: "+err.Error(), "").WithCode(output.CodeConfigUnavailable)
			}

			w.Success(fmt.Sprintf("removed %s", alias))
			return nil
		},
	}
}

func newConfigListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured hosts (alias for 'sshq ls')",
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
