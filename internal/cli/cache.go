package cli

import (
	"fmt"

	"github.com/shayuc137/sshq/internal/output"
	"github.com/spf13/cobra"
)

func newCacheCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage sshq local caches",
	}
	cmd.AddCommand(newCacheClearCommand())
	return cmd
}

func newCacheClearCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "clear [alias]",
		Short: "Clear cached remote shell profiles",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cache := profileCacheFrom(cmd.Context())
			if cache == nil {
				return output.Errorf("profile cache unavailable", "check the cache path and permissions")
			}

			cleared := 0
			if len(args) == 0 {
				cleared = cache.Clear()
			} else {
				store := configFrom(cmd.Context())
				if store == nil {
					return output.Errorf("no SSH config loaded", "check ~/.ssh/config exists")
				}
				host, err := store.Get(args[0])
				if err != nil {
					return output.Errorf(err.Error(), "run 'sshq ls' to see available hosts")
				}
				if _, ok := cache.Get(host.HostName, host.Port); ok {
					cleared = 1
				}
				cache.Invalidate(host.HostName, host.Port)
			}

			writerFrom(cmd.Context()).Render(cacheClearResult{Cleared: cleared})
			return nil
		},
	}
}

type cacheClearResult struct {
	Cleared int `json:"cleared"`
}

func (r cacheClearResult) Pretty() string {
	return fmt.Sprintf("Cleared %d cached profile(s)", r.Cleared)
}
