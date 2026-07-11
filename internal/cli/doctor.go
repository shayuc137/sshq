package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/shayuc137/sshq/internal/config"
	"github.com/shayuc137/sshq/internal/credential"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/remote"
	"github.com/shayuc137/sshq/internal/sshclient"
	"github.com/spf13/cobra"
)

type doctorCheckName string

const (
	doctorConfigValid   doctorCheckName = "config_valid"
	doctorIdentityFile  doctorCheckName = "identity_file_exists"
	doctorProxyJump     doctorCheckName = "proxy_jump_exists"
	doctorTCPReachable  doctorCheckName = "tcp_reachable"
	doctorHostKeyKnown  doctorCheckName = "host_key_known"
	doctorAuthOK        doctorCheckName = "auth_ok"
	doctorShellDetected doctorCheckName = "shell_detected"
)

type doctorCheckValue uint8

const (
	doctorCheckPassed doctorCheckValue = iota
	doctorCheckFailed
	doctorCheckSkipped
	doctorCheckNotApplicable
)

func (v doctorCheckValue) MarshalJSON() ([]byte, error) {
	switch v {
	case doctorCheckPassed:
		return []byte("true"), nil
	case doctorCheckFailed:
		return []byte("false"), nil
	case doctorCheckSkipped:
		return json.Marshal("skipped")
	case doctorCheckNotApplicable:
		return []byte("null"), nil
	default:
		return nil, fmt.Errorf("unknown doctor check value %d", v)
	}
}

func (v doctorCheckValue) prettySymbol() string {
	switch v {
	case doctorCheckPassed:
		return "✓"
	case doctorCheckFailed:
		return "✗"
	default:
		return "–"
	}
}

type doctorResolved struct {
	Hostname     string `json:"hostname"`
	Port         string `json:"port"`
	User         string `json:"user"`
	ProxyJump    string `json:"proxy_jump"`
	IdentityFile string `json:"identity_file"`
}

type doctorChecks struct {
	ConfigValid   doctorCheckValue `json:"config_valid"`
	IdentityFile  doctorCheckValue `json:"identity_file_exists"`
	ProxyJump     doctorCheckValue `json:"proxy_jump_exists"`
	TCPReachable  doctorCheckValue `json:"tcp_reachable"`
	HostKeyKnown  doctorCheckValue `json:"host_key_known"`
	AuthOK        doctorCheckValue `json:"auth_ok"`
	ShellDetected doctorCheckValue `json:"shell_detected"`
}

func newDoctorChecks() doctorChecks {
	return doctorChecks{
		ConfigValid:   doctorCheckSkipped,
		IdentityFile:  doctorCheckSkipped,
		ProxyJump:     doctorCheckSkipped,
		TCPReachable:  doctorCheckSkipped,
		HostKeyKnown:  doctorCheckSkipped,
		AuthOK:        doctorCheckSkipped,
		ShellDetected: doctorCheckSkipped,
	}
}

func (c *doctorChecks) set(name doctorCheckName, value doctorCheckValue) {
	switch name {
	case doctorConfigValid:
		c.ConfigValid = value
	case doctorIdentityFile:
		c.IdentityFile = value
	case doctorProxyJump:
		c.ProxyJump = value
	case doctorTCPReachable:
		c.TCPReachable = value
	case doctorHostKeyKnown:
		c.HostKeyKnown = value
	case doctorAuthOK:
		c.AuthOK = value
	case doctorShellDetected:
		c.ShellDetected = value
	}
}

func (c doctorChecks) ordered() []struct {
	name  doctorCheckName
	value doctorCheckValue
} {
	return []struct {
		name  doctorCheckName
		value doctorCheckValue
	}{
		{doctorConfigValid, c.ConfigValid},
		{doctorIdentityFile, c.IdentityFile},
		{doctorProxyJump, c.ProxyJump},
		{doctorTCPReachable, c.TCPReachable},
		{doctorHostKeyKnown, c.HostKeyKnown},
		{doctorAuthOK, c.AuthOK},
		{doctorShellDetected, c.ShellDetected},
	}
}

type doctorResult struct {
	Alias       string          `json:"alias"`
	Resolved    doctorResolved  `json:"resolved"`
	Checks      doctorChecks    `json:"checks"`
	Profile     *remote.Profile `json:"profile,omitempty"`
	FailedCheck string          `json:"failed_check,omitempty"`
	Hint        string          `json:"hint,omitempty"`
	NextAction  string          `json:"next_action,omitempty"`
}

func (r doctorResult) Pretty() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s@%s:%s\n", r.Alias, r.Resolved.User, r.Resolved.Hostname, r.Resolved.Port)
	if r.Resolved.ProxyJump != "" {
		fmt.Fprintf(&b, "ProxyJump: %s\n", r.Resolved.ProxyJump)
	}
	if r.Resolved.IdentityFile != "" {
		fmt.Fprintf(&b, "IdentityFile: %s\n", r.Resolved.IdentityFile)
	}
	for _, check := range r.Checks.ordered() {
		fmt.Fprintf(&b, "%s %s\n", check.value.prettySymbol(), check.name)
	}
	if r.Profile != nil {
		fmt.Fprintf(&b, "Profile: %s\n", remote.RenderProfileCompact(r.Profile))
	}
	if r.Hint != "" {
		fmt.Fprintf(&b, "Hint: %s\n", r.Hint)
	}
	if r.NextAction != "" {
		fmt.Fprintf(&b, "Next action: %s\n", r.NextAction)
	}
	return strings.TrimRight(b.String(), "\n")
}

type doctorFailureKind string

const (
	doctorFailureConfigMissing   doctorFailureKind = "config_missing"
	doctorFailureConfigInvalid   doctorFailureKind = "config_invalid"
	doctorFailureIdentityMissing doctorFailureKind = "identity_missing"
	doctorFailureProxyMissing    doctorFailureKind = "proxy_missing"
	doctorFailureTCP             doctorFailureKind = "tcp"
	doctorFailureHostKeyUnknown  doctorFailureKind = "host_key_unknown"
	doctorFailureHostKeyMismatch doctorFailureKind = "host_key_mismatch"
	doctorFailureHostKey         doctorFailureKind = "host_key"
	doctorFailureAuth            doctorFailureKind = "auth"
	doctorFailureShell           doctorFailureKind = "shell"
)

type doctorCheckOutcome struct {
	Value     doctorCheckValue
	Kind      doctorFailureKind
	Key       string
	ValueHint string
	Hint      string
}

type doctorRunState struct {
	alias     string
	host      config.Host
	connCfg   sshclient.ConnConfig
	tcpConn   net.Conn
	tcpCloser io.Closer
	client    *sshclient.Client
	profile   *remote.Profile
}

type doctorChecker interface {
	Check(context.Context, doctorCheckName, *doctorRunState) doctorCheckOutcome
}

type doctorCheckSpec struct {
	name          doctorCheckName
	stopOnFailure bool
}

var doctorCheckOrder = []doctorCheckSpec{
	{name: doctorConfigValid, stopOnFailure: true},
	{name: doctorIdentityFile},
	{name: doctorProxyJump, stopOnFailure: true},
	{name: doctorTCPReachable, stopOnFailure: true},
	{name: doctorHostKeyKnown, stopOnFailure: true},
	{name: doctorAuthOK, stopOnFailure: true},
	{name: doctorShellDetected},
}

func runDoctor(ctx context.Context, alias string, checker doctorChecker) doctorResult {
	state := &doctorRunState{alias: alias}
	result := doctorResult{Alias: alias, Checks: newDoctorChecks()}
	defer func() {
		if state.tcpCloser != nil {
			state.tcpCloser.Close()
		}
		if state.client != nil {
			state.client.Close()
		}
	}()

	for _, spec := range doctorCheckOrder {
		outcome := checker.Check(ctx, spec.name, state)
		result.Checks.set(spec.name, outcome.Value)
		if state.host.Alias != "" {
			result.Resolved = resolvedDoctorHost(state.host)
		}
		if state.profile != nil {
			result.Profile = state.profile
		}
		if outcome.Value != doctorCheckFailed {
			continue
		}
		if result.FailedCheck == "" {
			result.FailedCheck = string(spec.name)
			result.Hint = outcome.Hint
			result.NextAction = doctorNextAction(alias, outcome)
		}
		if spec.stopOnFailure {
			break
		}
	}

	return result
}

func resolvedDoctorHost(host config.Host) doctorResolved {
	return doctorResolved{
		Hostname:     host.HostName,
		Port:         host.Port,
		User:         host.User,
		ProxyJump:    host.ProxyJump,
		IdentityFile: credential.ExpandHome(host.IdentityFile),
	}
}

func doctorNextAction(alias string, outcome doctorCheckOutcome) string {
	switch outcome.Kind {
	case doctorFailureConfigMissing:
		return fmt.Sprintf("sshq config add %s --hostname <ip>", alias)
	case doctorFailureConfigInvalid:
		return fmt.Sprintf("sshq config set %s %s <value>", alias, outcome.Key)
	case doctorFailureIdentityMissing:
		return fmt.Sprintf("sshq config set %s identityfile <existing-path>", alias)
	case doctorFailureProxyMissing:
		if outcome.ValueHint == "" {
			return ""
		}
		return fmt.Sprintf("sshq config add %s --hostname <ip>", outcome.ValueHint)
	case doctorFailureHostKeyUnknown:
		return fmt.Sprintf("sshq trust %s", alias)
	case doctorFailureHostKeyMismatch:
		return fmt.Sprintf("sshq trust %s --replace", alias)
	default:
		return ""
	}
}

type doctorCheckerFactory func(*config.Store, *credential.Store, *remote.Cache, time.Duration) doctorChecker

func newDoctorCommand() *cobra.Command {
	return newDoctorCommandWithChecker(newSystemDoctorChecker)
}

func newDoctorCommandWithChecker(factory doctorCheckerFactory) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor <alias>",
		Short: "Diagnose SSH configuration and connectivity",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store := configFrom(cmd.Context())
			if store == nil {
				return output.Errorf("no SSH config loaded", "check ~/.ssh/config exists").WithCode(output.CodeConfigUnavailable)
			}
			timeout, _ := cmd.Flags().GetDuration("timeout")
			checker := factory(store, credentialStoreFrom(cmd.Context()), profileCacheFrom(cmd.Context()), timeout)
			result := runDoctor(cmd.Context(), args[0], checker)
			writerFrom(cmd.Context()).Render(result)
			if result.FailedCheck != "" {
				return output.BadNews()
			}
			return nil
		},
	}
}
