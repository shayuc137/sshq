package cli

import (
	"fmt"

	"github.com/shayuc137/sshq/internal/config"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/remote"
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
			cache := profileCacheFrom(cmd.Context())
			var profile *remote.Profile
			if cache != nil {
				profile, _ = cache.Get(host.HostName, host.Port)
			}

			if w.IsJSONMode() {
				m := hostsToMaps([]config.Host{host})
				data := m[0]
				if profile != nil {
					data["profile"] = profile
				}
				w.JSONOut(data)
				return nil
			}

			pretty, _ := cmd.Flags().GetBool("pretty")
			if pretty {
				s := config.RenderInfoPretty(host)
				if profile != nil {
					s += renderProfilePretty(profile)
				}
				w.Value(s)
			} else {
				s := config.RenderInfoCompact(host)
				if profile != nil {
					s = appendProfileCompact(s, profile)
				}
				w.Value(s)
			}
			return nil
		},
	}
}

func renderProfilePretty(p *remote.Profile) string {
	s := "---\n"
	s += fmt.Sprintf("OS:           %s\n", p.OS)
	s += fmt.Sprintf("Shell:        %s\n", p.Shell)
	if p.Encoding != "" {
		s += fmt.Sprintf("Encoding:     %s\n", p.Encoding)
	}
	if p.HomeDir != "" {
		s += fmt.Sprintf("RemoteHome:   %s\n", p.HomeDir)
	}
	return s
}

func appendProfileCompact(s string, p *remote.Profile) string {
	s = s[:len(s)-1] // strip trailing \n
	s += fmt.Sprintf(" os=%s shell=%s", p.OS, p.Shell)
	if p.Encoding != "" {
		s += " encoding=" + p.Encoding
	}
	return s + "\n"
}
