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

type trustOutcome int

const (
	trustAlready trustOutcome = iota
	trustAdded
	trustReplaced
	trustMismatch
	trustFailed
)

type trustFailureStep string

const (
	trustResolveFailed trustFailureStep = "resolve"
	trustFetchFailed   trustFailureStep = "fetch"
	trustAddFailed     trustFailureStep = "add"
	trustRemoveFailed  trustFailureStep = "remove"
)

type trustResult struct {
	Alias      string
	Addr       string
	KeyInfo    string
	OldKeyInfo string
	Outcome    trustOutcome
	Failure    trustFailureStep
	Err        error
}

func trustOne(w *output.Writer, store *config.Store, alias string, replace bool, timeout time.Duration) error {
	return renderTrustOne(w, trustHost(store, alias, replace, timeout))
}

func trustAll(w *output.Writer, store *config.Store, replace bool, timeout time.Duration) error {
	hosts := store.List()
	var summary trustSummary

	for _, host := range hosts {
		result := trustHost(store, host.Alias, replace, timeout)
		renderTrustAllLine(w, result)
		summary.Add(result)
	}

	w.Success(summary.String(len(hosts)))
	return nil
}

func trustHost(store *config.Store, alias string, replace bool, timeout time.Duration) trustResult {
	result := trustResult{Alias: alias}
	host, err := store.Get(alias)
	if err != nil {
		result.Outcome = trustFailed
		result.Failure = trustResolveFailed
		result.Err = err
		return result
	}

	addr := net.JoinHostPort(host.HostName, host.Port)
	result.Addr = addr

	check, err := hostkey.FetchAndCheck(addr, timeout)
	if err != nil {
		result.Outcome = trustFailed
		result.Failure = trustFetchFailed
		result.Err = err
		return result
	}
	result.KeyInfo = fmt.Sprintf("%s %s", hostkey.KeyType(check.Key), hostkey.Fingerprint(check.Key))

	switch check.Status {
	case hostkey.Trusted:
		result.Outcome = trustAlready
	case hostkey.Unknown:
		if err := hostkey.Add(addr, check.Key); err != nil {
			result.Outcome = trustFailed
			result.Failure = trustAddFailed
			result.Err = err
			return result
		}
		result.Outcome = trustAdded
	case hostkey.Mismatch:
		if len(check.Want) > 0 {
			result.OldKeyInfo = fmt.Sprintf("%s %s",
				check.Want[0].Key.Type(),
				hostkey.Fingerprint(check.Want[0].Key))
		}
		if !replace {
			result.Outcome = trustMismatch
			return result
		}
		if _, err := hostkey.Remove(addr); err != nil {
			result.Outcome = trustFailed
			result.Failure = trustRemoveFailed
			result.Err = err
			return result
		}
		if err := hostkey.Add(addr, check.Key); err != nil {
			result.Outcome = trustFailed
			result.Failure = trustAddFailed
			result.Err = err
			return result
		}
		result.Outcome = trustReplaced
	}

	return result
}

func renderTrustOne(w *output.Writer, result trustResult) error {
	switch result.Outcome {
	case trustAlready:
		w.Success(fmt.Sprintf("%s already trusted (%s)", result.Alias, result.KeyInfo))
	case trustAdded:
		w.Success(fmt.Sprintf("%s trusted (%s)", result.Alias, result.KeyInfo))
	case trustReplaced:
		w.Success(fmt.Sprintf("%s key replaced (%s)", result.Alias, result.KeyInfo))
	case trustMismatch:
		hint := fmt.Sprintf("host key CHANGED for %s (%s)", result.Alias, result.Addr)
		if result.OldKeyInfo != "" {
			hint += "\n  old: " + result.OldKeyInfo
		}
		hint += "\n  new: " + result.KeyInfo
		return output.Errorf(hint, "if expected (e.g. OS reinstall), re-run with --replace")
	case trustFailed:
		return renderTrustFailure(result)
	}
	return nil
}

func renderTrustFailure(result trustResult) error {
	switch result.Failure {
	case trustResolveFailed:
		return output.Errorf(result.Err.Error(), "run 'sshq ls' to see available hosts")
	case trustFetchFailed:
		return output.Errorf(fmt.Sprintf("cannot reach %s (%s)", result.Alias, result.Addr), result.Err.Error())
	case trustAddFailed:
		return output.Errorf("failed to add key", result.Err.Error())
	case trustRemoveFailed:
		return output.Errorf("failed to remove old key", result.Err.Error())
	default:
		return output.Errorf(result.Err.Error(), "")
	}
}

func renderTrustAllLine(w *output.Writer, result trustResult) {
	switch result.Outcome {
	case trustAlready:
		w.Info(fmt.Sprintf("%s already trusted", result.Alias))
	case trustAdded:
		w.Info(fmt.Sprintf("%s trusted (%s)", result.Alias, result.KeyInfo))
	case trustReplaced:
		w.Info(fmt.Sprintf("%s key replaced (%s)", result.Alias, result.KeyInfo))
	case trustMismatch:
		w.Info(fmt.Sprintf("%s key CHANGED — skipped (use --replace to update)", result.Alias))
	case trustFailed:
		switch result.Failure {
		case trustFetchFailed:
			w.Info(fmt.Sprintf("%s (%s) unreachable: %s", result.Alias, result.Addr, result.Err))
		case trustAddFailed:
			w.Info(fmt.Sprintf("%s failed to add: %s", result.Alias, result.Err))
		case trustRemoveFailed:
			w.Info(fmt.Sprintf("%s failed to remove old key: %s", result.Alias, result.Err))
		default:
			w.Info(fmt.Sprintf("%s failed: %s", result.Alias, result.Err))
		}
	}
}

type trustSummary struct {
	Trusted  int
	Added    int
	Replaced int
	Failed   int
	Mismatch int
}

func (s *trustSummary) Add(result trustResult) {
	switch result.Outcome {
	case trustAlready:
		s.Trusted++
	case trustAdded:
		s.Added++
	case trustReplaced:
		s.Replaced++
	case trustMismatch:
		s.Mismatch++
	case trustFailed:
		s.Failed++
	}
}

func (s trustSummary) String(total int) string {
	summary := fmt.Sprintf("total=%d trusted=%d added=%d", total, s.Trusted, s.Added)
	if s.Replaced > 0 {
		summary += fmt.Sprintf(" replaced=%d", s.Replaced)
	}
	if s.Mismatch > 0 {
		summary += fmt.Sprintf(" mismatch=%d", s.Mismatch)
	}
	if s.Failed > 0 {
		summary += fmt.Sprintf(" failed=%d", s.Failed)
	}
	return summary
}
