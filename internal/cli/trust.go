package cli

import (
	"fmt"
	"net"
	"time"

	"github.com/shayuc137/sshq/internal/config"
	"github.com/shayuc137/sshq/internal/hostkey"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/spf13/cobra"
)

func newTrustCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trust [alias]",
		Short: "Fetch and trust a host's SSH key",
		Long: `Fetch the SSH host key from a remote server and add it to known_hosts.
If the key has changed (mismatch), use --replace to update it.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			all, _ := cmd.Flags().GetBool("all")
			replace, _ := cmd.Flags().GetBool("replace")
			timeout, _ := cmd.Flags().GetDuration("timeout")
			w := writerFrom(cmd.Context())
			store := configFrom(cmd.Context())

			if store == nil {
				return output.Errorf("no SSH config loaded", "check ~/.ssh/config exists")
			}

			if all {
				return trustAll(w, store, replace, timeout)
			}

			if len(args) == 0 {
				return output.Errorf("alias required", "usage: sshq trust <alias> or sshq trust --all")
			}

			return trustOne(w, store, args[0], replace, timeout)
		},
	}

	cmd.Flags().Bool("all", false, "trust all configured hosts")
	cmd.Flags().Bool("replace", false, "replace mismatched host keys")
	return cmd
}

func trustOne(w *output.Writer, store *config.Store, alias string, replace bool, timeout time.Duration) error {
	host, err := store.Get(alias)
	if err != nil {
		return output.Errorf(err.Error(), "run 'sshq ls' to see available hosts")
	}

	addr := net.JoinHostPort(host.HostName, host.Port)

	result, err := hostkey.FetchAndCheck(addr, timeout)
	if err != nil {
		return output.Errorf(
			fmt.Sprintf("cannot reach %s (%s)", alias, addr),
			err.Error(),
		)
	}

	keyInfo := fmt.Sprintf("%s %s", hostkey.KeyType(result.Key), hostkey.Fingerprint(result.Key))

	switch result.Status {
	case hostkey.Trusted:
		w.Success(fmt.Sprintf("%s already trusted (%s)", alias, keyInfo))

	case hostkey.Unknown:
		if err := hostkey.Add(addr, result.Key); err != nil {
			return output.Errorf("failed to add key", err.Error())
		}
		w.Success(fmt.Sprintf("%s trusted (%s)", alias, keyInfo))

	case hostkey.Mismatch:
		if !replace {
			hint := fmt.Sprintf("host key CHANGED for %s (%s)", alias, addr)
			if len(result.Want) > 0 {
				hint += fmt.Sprintf("\n  old: %s %s",
					result.Want[0].Key.Type(),
					hostkey.Fingerprint(result.Want[0].Key))
			}
			hint += fmt.Sprintf("\n  new: %s", keyInfo)
			return output.Errorf(hint, "if expected (e.g. OS reinstall), re-run with --replace")
		}
		if _, err := hostkey.Remove(addr); err != nil {
			return output.Errorf("failed to remove old key", err.Error())
		}
		if err := hostkey.Add(addr, result.Key); err != nil {
			return output.Errorf("failed to add new key", err.Error())
		}
		w.Success(fmt.Sprintf("%s key replaced (%s)", alias, keyInfo))
	}

	return nil
}

func trustAll(w *output.Writer, store *config.Store, replace bool, timeout time.Duration) error {
	hosts := store.List()
	trusted, added, replaced, failed, mismatch := 0, 0, 0, 0, 0

	for _, host := range hosts {
		addr := net.JoinHostPort(host.HostName, host.Port)
		result, err := hostkey.FetchAndCheck(addr, timeout)
		if err != nil {
			w.Info(fmt.Sprintf("%s (%s) unreachable: %s", host.Alias, addr, err))
			failed++
			continue
		}

		keyInfo := fmt.Sprintf("%s %s", hostkey.KeyType(result.Key), hostkey.Fingerprint(result.Key))

		switch result.Status {
		case hostkey.Trusted:
			w.Info(fmt.Sprintf("%s already trusted", host.Alias))
			trusted++

		case hostkey.Unknown:
			if err := hostkey.Add(addr, result.Key); err != nil {
				w.Info(fmt.Sprintf("%s failed to add: %s", host.Alias, err))
				failed++
				continue
			}
			w.Info(fmt.Sprintf("%s trusted (%s)", host.Alias, keyInfo))
			added++

		case hostkey.Mismatch:
			if !replace {
				w.Info(fmt.Sprintf("%s key CHANGED — skipped (use --replace to update)", host.Alias))
				mismatch++
				continue
			}
			if _, err := hostkey.Remove(addr); err != nil {
				w.Info(fmt.Sprintf("%s failed to remove old key: %s", host.Alias, err))
				failed++
				continue
			}
			if err := hostkey.Add(addr, result.Key); err != nil {
				w.Info(fmt.Sprintf("%s failed to add new key: %s", host.Alias, err))
				failed++
				continue
			}
			w.Info(fmt.Sprintf("%s key replaced (%s)", host.Alias, keyInfo))
			replaced++
		}
	}

	summary := fmt.Sprintf("total=%d trusted=%d added=%d", len(hosts), trusted, added)
	if replaced > 0 {
		summary += fmt.Sprintf(" replaced=%d", replaced)
	}
	if mismatch > 0 {
		summary += fmt.Sprintf(" mismatch=%d", mismatch)
	}
	if failed > 0 {
		summary += fmt.Sprintf(" failed=%d", failed)
	}
	w.Success(summary)
	return nil
}
