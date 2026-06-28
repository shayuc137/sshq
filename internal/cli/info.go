package cli

import (
	"fmt"
	"strings"

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

			w.Render(hostInfo{Host: host, Profile: profile})
			return nil
		},
	}
}

// hostInfo bundles a host with its cached profile for the info command.
// The embedded Host's json fields are promoted, matching the prior output.
type hostInfo struct {
	config.Host
	Profile *remote.Profile `json:"profile,omitempty"`
}

func (hi hostInfo) Pretty() string {
	var b strings.Builder
	b.WriteString(config.HostDetail(hi.Host).Pretty())
	if hi.Profile != nil {
		b.WriteString("---\n")
		fmt.Fprintf(&b, "OS:           %s\n", hi.Profile.OS)
		fmt.Fprintf(&b, "Shell:        %s\n", hi.Profile.Shell)
		if hi.Profile.Encoding != "" {
			fmt.Fprintf(&b, "Encoding:     %s\n", hi.Profile.Encoding)
		}
		if hi.Profile.HomeDir != "" {
			fmt.Fprintf(&b, "RemoteHome:   %s\n", hi.Profile.HomeDir)
		}
	}
	return b.String()
}
