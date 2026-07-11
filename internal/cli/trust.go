package cli

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/shayuc137/sshq/internal/config"
	"github.com/shayuc137/sshq/internal/credential"
	"github.com/shayuc137/sshq/internal/hostkey"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/sshclient"
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
				return output.Errorf("no SSH config loaded", "check ~/.ssh/config exists").WithCode(output.CodeConfigUnavailable)
			}

			if all {
				return trustAll(cmd.Context(), w, store, credentialStoreFrom(cmd.Context()), replace, timeout)
			}

			if len(args) == 0 {
				return output.Errorf("alias required", "usage: sshq trust <alias> or sshq trust --all").WithCode(output.CodeInvalidUsage)
			}

			return trustOne(cmd.Context(), w, store, credentialStoreFrom(cmd.Context()), args[0], replace, timeout)
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

func (o trustOutcome) String() string {
	switch o {
	case trustAlready:
		return "trusted"
	case trustAdded:
		return "added"
	case trustReplaced:
		return "replaced"
	case trustMismatch:
		return "mismatch"
	case trustFailed:
		return "failed"
	default:
		return "unknown"
	}
}

type trustFailureStep string

const (
	trustResolveFailed trustFailureStep = "resolve"
	trustConfigFailed  trustFailureStep = "config"
	trustFetchFailed   trustFailureStep = "fetch"
	trustAddFailed     trustFailureStep = "add"
	trustRemoveFailed  trustFailureStep = "remove"
)

type trustResult struct {
	Alias             string           `json:"alias"`
	Target            string           `json:"target"`
	HostName          string           `json:"hostname"`
	Port              string           `json:"port"`
	ProxyJump         string           `json:"proxy_jump"`
	LookupAlias       string           `json:"lookup_alias"`
	LookupKeys        []string         `json:"lookup_keys"`
	RemoteFingerprint string           `json:"remote_fingerprint,omitempty"`
	KnownFingerprint  string           `json:"known_fingerprint,omitempty"`
	KeyInfo           string           `json:"key_info,omitempty"`
	OldKeyInfo        string           `json:"old_key_info,omitempty"`
	Status            string           `json:"status"`
	Outcome           trustOutcome     `json:"-"`
	Failure           trustFailureStep `json:"-"`
	Err               error            `json:"-"`
}

func (r trustResult) Pretty() string {
	var message string
	switch r.Outcome {
	case trustAlready:
		message = fmt.Sprintf("%s already trusted (%s)", r.Alias, r.KeyInfo)
	case trustAdded:
		message = fmt.Sprintf("%s trusted (%s)", r.Alias, r.KeyInfo)
	case trustReplaced:
		message = fmt.Sprintf("%s key replaced (%s)", r.Alias, r.KeyInfo)
	default:
		message = fmt.Sprintf("%s %s", r.Alias, r.Status)
	}
	proxyJump := r.ProxyJump
	if proxyJump == "" {
		proxyJump = "(none)"
	}
	return fmt.Sprintf("%s\ntarget=%s proxy_jump=%s lookup_alias=%s", message, r.Target, proxyJump, r.LookupAlias)
}

func trustResultDetails(result trustResult) map[string]any {
	return hostKeyErrorDetails(
		result.Alias, result.HostName, result.Port, result.ProxyJump, result.LookupKeys,
		result.RemoteFingerprint, result.KnownFingerprint,
	)
}

func trustOne(ctx context.Context, w *output.Writer, store *config.Store, creds *credential.Store, alias string, replace bool, timeout time.Duration) error {
	return renderTrustOne(w, trustHost(ctx, store, creds, alias, replace, timeout))
}

func trustAll(ctx context.Context, w *output.Writer, store *config.Store, creds *credential.Store, replace bool, timeout time.Duration) error {
	hosts := store.List()
	var summary trustSummary

	for _, host := range hosts {
		result := trustHost(ctx, store, creds, host.Alias, replace, timeout)
		renderTrustAllLine(w, result)
		summary.Add(result)
	}

	w.Success(summary.String(len(hosts)))
	return nil
}

var dialTrustTCP = sshclient.DialTCP
var fetchTrustHostKey = hostkey.FetchConn

func trustHost(ctx context.Context, store *config.Store, creds *credential.Store, alias string, replace bool, timeout time.Duration) (result trustResult) {
	result = trustResult{Alias: alias, LookupAlias: alias, LookupKeys: []string{}}
	defer func() { result.Status = result.Outcome.String() }()
	host, err := store.Get(alias)
	if err != nil {
		result.Outcome = trustFailed
		result.Failure = trustResolveFailed
		result.Err = err
		return result
	}

	addr := net.JoinHostPort(host.HostName, host.Port)
	result.Target = addr
	result.HostName = host.HostName
	result.Port = host.Port
	result.ProxyJump = host.ProxyJump
	result.LookupKeys = hostkey.LookupKeys(alias, host.HostName, host.Port)

	cfg, err := hostToConnConfigWithCredentials(host, store, creds)
	if err != nil {
		result.Outcome = trustFailed
		result.Failure = trustConfigFailed
		result.Err = err
		return result
	}
	cfg.Timeout = timeout
	conn, closer, err := dialTrustTCP(ctx, cfg)
	if err != nil {
		result.Outcome = trustFailed
		result.Failure = trustFetchFailed
		result.Err = err
		return result
	}
	defer closer.Close()

	key, err := fetchTrustHostKey(conn, addr, timeout)
	if err != nil {
		result.Outcome = trustFailed
		result.Failure = trustFetchFailed
		result.Err = err
		return result
	}
	check, err := hostkey.Check(addr, key)
	if err != nil {
		result.Outcome = trustFailed
		result.Failure = trustFetchFailed
		result.Err = err
		return result
	}
	result.RemoteFingerprint = hostkey.Fingerprint(check.Key)
	result.KeyInfo = fmt.Sprintf("%s %s", hostkey.KeyType(check.Key), hostkey.Fingerprint(check.Key))

	switch check.Status {
	case hostkey.Trusted:
		result.KnownFingerprint = result.RemoteFingerprint
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
			result.KnownFingerprint = hostkey.Fingerprint(check.Want[0].Key)
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
		w.Render(result)
	case trustAdded:
		w.Render(result)
	case trustReplaced:
		w.Render(result)
	case trustMismatch:
		hint := fmt.Sprintf("host key CHANGED for %s (%s)", result.Alias, result.Target)
		if result.OldKeyInfo != "" {
			hint += "\n  old: " + result.OldKeyInfo
		}
		hint += "\n  new: " + result.KeyInfo
		return output.Errorf(hint, fmt.Sprintf("run: sshq trust %s --replace", result.Alias)).WithCode(output.CodeHostKeyMismatch).WithDetails(trustResultDetails(result))
	case trustFailed:
		return renderTrustFailure(result)
	}
	return nil
}

func renderTrustFailure(result trustResult) error {
	switch result.Failure {
	case trustResolveFailed:
		return output.Errorf(result.Err.Error(), "run: sshq ls").WithCode(output.CodeHostNotFound)
	case trustConfigFailed:
		return output.Errorf(credentialErrorSummary(result.Err), "").WithCode(output.CodeCredentialError)
	case trustFetchFailed:
		return output.Errorf(fmt.Sprintf("cannot reach %s (%s): %v", result.Alias, result.Target, result.Err), "").WithCode(output.CodeNetworkError)
	case trustAddFailed:
		return output.Errorf("failed to add key: "+result.Err.Error(), "").WithCode(output.CodeInternalError)
	case trustRemoveFailed:
		return output.Errorf("failed to remove old key: "+result.Err.Error(), "").WithCode(output.CodeInternalError)
	default:
		return output.Errorf(result.Err.Error(), "").WithCode(output.CodeInternalError)
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
			w.Info(fmt.Sprintf("%s (%s) unreachable: %s", result.Alias, result.Target, result.Err))
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
