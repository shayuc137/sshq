package cli

import (
	"bufio"
	"fmt"
	"strings"
	"time"

	"github.com/shayuc137/sshq/internal/appconfig"
	"github.com/shayuc137/sshq/internal/ipc"
	"github.com/shayuc137/sshq/internal/output"
	policypkg "github.com/shayuc137/sshq/internal/policy"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newPolicyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Inspect and manage capability policy",
	}
	cmd.AddCommand(
		newPolicyGrantCommand(),
		newPolicyRevokeCommand(),
		newPolicyListCommand(),
		newPolicyValidateCommand(),
		newPolicyCheckCommand(),
	)
	return cmd
}

func newPolicyGrantCommand() *cobra.Command {
	var ttl time.Duration
	var kind string
	cmd := &cobra.Command{
		Use:   "grant <alias> <pattern>",
		Short: "Temporarily grant a policy whitelist exception in the daemon",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			alias, pattern := args[0], args[1]
			if ttl <= 0 {
				return output.Errorf("--ttl is required", "example: sshq policy grant prod '^uptime$' --ttl 15m").WithCode(output.CodeInvalidUsage)
			}
			if ttl > policypkg.MaxGrantTTL {
				return output.Errorf("ttl exceeds maximum "+policypkg.MaxGrantTTL.String(), "use a shorter --ttl").WithCode(output.CodeInvalidUsage)
			}
			if !validPolicyKind(kind) {
				return output.Errorf("invalid grant kind: "+kind, "use command, local-path, remote-path, local-forward, or remote-forward").WithCode(output.CodeInvalidUsage)
			}
			if err := confirmPolicyGrant(cmd, alias, kind, pattern, ttl); err != nil {
				return output.Errorf(err.Error(), "run in an interactive terminal with a controlling TTY").WithCode(output.CodeInvalidUsage)
			}

			var result ipc.PolicyGrantResult
			if err := sendPolicyRequest("policy-grant", ipc.PolicyGrantPayload{
				Alias:      alias,
				Kind:       kind,
				Pattern:    pattern,
				TTLSeconds: int(ttl.Seconds()),
			}, &result); err != nil {
				return err
			}
			writerFrom(cmd.Context()).Render(policyGrantOutput(result))
			return nil
		},
	}
	cmd.Flags().DurationVar(&ttl, "ttl", 0, "grant TTL, required and capped at 1h")
	cmd.Flags().StringVar(&kind, "kind", policypkg.KindCommand, "grant kind: command, local-path, remote-path, local-forward, or remote-forward")
	return cmd
}

func newPolicyRevokeCommand() *cobra.Command {
	var alias string
	cmd := &cobra.Command{
		Use:   "revoke [grant-id]",
		Short: "Revoke temporary policy grants",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if alias != "" && len(args) > 0 {
				return output.Errorf("--alias cannot be combined with grant id", "use either sshq policy revoke <grant-id> or sshq policy revoke --alias <alias>").WithCode(output.CodeInvalidUsage)
			}
			id := ""
			if len(args) == 1 {
				id = args[0]
			}
			if id == "" && alias == "" {
				return output.Errorf("grant id or --alias required", "use sshq policy list to see active grants").WithCode(output.CodeInvalidUsage)
			}

			var result ipc.PolicyRevokeResult
			if err := sendPolicyRequest("policy-revoke", ipc.PolicyRevokePayload{ID: id, Alias: alias}, &result); err != nil {
				return err
			}
			writerFrom(cmd.Context()).Render(policyRevokeOutput(result))
			return nil
		},
	}
	cmd.Flags().StringVar(&alias, "alias", "", "revoke all grants for an alias")
	return cmd
}

func newPolicyListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list [alias]",
		Short: "List effective policy and temporary daemon grants",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			alias := ""
			if len(args) == 1 {
				alias = args[0]
			}

			var result ipc.PolicyListResult
			if ipc.IsRunning() {
				if err := sendPolicyRequest("policy-list", ipc.PolicyListPayload{Alias: alias}, &result); err != nil {
					return err
				}
			} else {
				local, err := localPolicyList(cmd, alias)
				if err != nil {
					return err
				}
				result = local
			}
			writerFrom(cmd.Context()).Render(policyListOutput(result))
			return nil
		},
	}
}

func newPolicyValidateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate config.toml policy syntax and references",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := appconfig.Load()
			if err != nil {
				return output.Errorf("policy config invalid: "+err.Error(), "fix config.toml").WithCode(output.CodeConfigUnavailable)
			}
			store := configFrom(cmd.Context())
			var aliasExists func(string) bool
			if store != nil {
				aliasExists = func(alias string) bool {
					_, err := store.Get(alias)
					return err == nil
				}
			}

			errs := policypkg.ValidateConfig(cfg, aliasExists)
			if len(errs) > 0 {
				return output.Errorf("policy config invalid: "+joinValidationErrors(errs), "fix "+cfg.Path()).WithCode(output.CodeConfigUnavailable)
			}
			if !cfg.Exists() {
				writerFrom(cmd.Context()).Success("config.toml not found; policy disabled")
				return nil
			}
			writerFrom(cmd.Context()).Success("policy config valid: " + cfg.Path())
			return nil
		},
	}
}

func newPolicyCheckCommand() *cobra.Command {
	var command string
	var localPath string
	var remotePath string
	var localForward string
	var remoteForward string
	cmd := &cobra.Command{
		Use:   "check <alias>",
		Short: "Check whether a command, path, or forward target would be allowed",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			alias := args[0]
			selected := 0
			for _, v := range []string{command, localPath, remotePath, localForward, remoteForward} {
				if v != "" {
					selected++
				}
			}
			if selected != 1 {
				return output.Errorf("exactly one check input is required", "use --command, --local-path, --remote-path, --local-forward, or --remote-forward").WithCode(output.CodeInvalidUsage)
			}

			checker, err := checkerForPolicyCheck(cmd.Context())
			if err != nil {
				return err
			}
			if checker == nil {
				checker = policypkg.NewChecker(appConfigFrom(cmd.Context()), nil)
			}

			var decision policypkg.Decision
			switch {
			case command != "":
				decision = checker.CheckCommand(alias, command)
			case localPath != "":
				decision = checker.CheckLocalPath(alias, localPath)
			case remotePath != "":
				decision = checker.CheckRemotePath(alias, remotePath)
			case localForward != "":
				decision = checker.CheckLocalForward(alias, localForward)
			case remoteForward != "":
				decision = checker.CheckRemoteForward(alias, remoteForward)
			}

			writerFrom(cmd.Context()).Render(policyCheckOutput{Decision: decision})
			return nil
		},
	}
	cmd.Flags().StringVar(&command, "command", "", "command text to check")
	cmd.Flags().StringVar(&localPath, "local-path", "", "local path to check")
	cmd.Flags().StringVar(&remotePath, "remote-path", "", "remote path to check")
	cmd.Flags().StringVar(&localForward, "local-forward", "", "local forward target (host:port) to check")
	cmd.Flags().StringVar(&remoteForward, "remote-forward", "", "remote forward target (host:port) to check")
	return cmd
}

func sendPolicyRequest(action string, payload any, out any) error {
	conn, err := ipc.Connect()
	if err != nil {
		return output.Errorf("daemon not running", "start daemon with: sshq daemon start").WithCode(output.CodeDaemonError)
	}
	defer conn.Close()

	env, _ := ipc.MakeEnvelope(action, payload)
	if err := ipc.Send(conn, env); err != nil {
		return output.Errorf("send "+action+": "+err.Error(), "restart daemon and retry").WithCode(output.CodeDaemonError)
	}
	err = recvPolicyResult(conn, out)
	// A grant mutates daemon state: once the request is on the wire, a lost
	// response means the grant may already be active, so the caller must
	// verify before re-issuing. Revoke is idempotent and list is read-only,
	// so a plain daemon_error (safe to retry) stays correct for them.
	if err != nil && action == "policy-grant" {
		if ce, ok := err.(*output.CmdError); ok && ce.Code == output.CodeDaemonError && strings.Contains(ce.Hint, "daemon connection lost") {
			return output.Errorf("daemon connection lost after grant was sent; the grant may already be active",
				"run 'sshq policy list' to verify before granting again").WithCode(output.CodeResultIndeterminate)
		}
	}
	return err
}

func localPolicyList(cmd *cobra.Command, alias string) (ipc.PolicyListResult, error) {
	if err := appConfigErrorFrom(cmd.Context()); err != nil {
		return ipc.PolicyListResult{}, output.Errorf("app config invalid: "+err.Error(), "fix config.toml").WithCode(output.CodeConfigUnavailable)
	}
	if alias != "" {
		store := configFrom(cmd.Context())
		if store != nil {
			if _, err := store.Get(alias); err != nil {
				return ipc.PolicyListResult{}, output.Errorf(err.Error(), "run 'sshq ls' to see available hosts").WithCode(output.CodeHostNotFound)
			}
		}
	}
	checker := policyCheckerFrom(cmd.Context())
	if checker == nil {
		checker = policypkg.NewChecker(appConfigFrom(cmd.Context()), nil)
	}
	return ipc.PolicyListResult{
		Alias:  alias,
		Policy: policyToIPC(checker.EffectivePolicy(alias)),
		Grants: []ipc.PolicyGrantInfo{},
	}, nil
}

func confirmPolicyGrant(cmd *cobra.Command, alias, kind, pattern string, ttl time.Duration) error {
	tty, err := openControlTerminal()
	if err != nil {
		return fmt.Errorf("policy grant requires a controlling terminal: %w", err)
	}
	defer tty.Close()
	if !term.IsTerminal(int(tty.Fd())) {
		return fmt.Errorf("policy grant requires a real TTY")
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "Temporary policy grant:\n  alias: %s\n  kind: %s\n  pattern: %s\n  ttl: %s\nType %q to confirm: ", alias, kind, pattern, ttl, alias)
	line, err := bufio.NewReader(tty).ReadString('\n')
	fmt.Fprintln(cmd.ErrOrStderr())
	if err != nil {
		return fmt.Errorf("read confirmation: %w", err)
	}
	if strings.TrimSpace(line) != alias {
		return fmt.Errorf("confirmation mismatch")
	}
	return nil
}

func validPolicyKind(kind string) bool {
	switch kind {
	case policypkg.KindCommand, policypkg.KindLocalPath, policypkg.KindRemotePath,
		policypkg.KindLocalForward, policypkg.KindRemoteForward:
		return true
	default:
		return false
	}
}

func joinValidationErrors(errs []policypkg.ValidationError) string {
	parts := make([]string, 0, len(errs))
	for _, err := range errs {
		parts = append(parts, err.Error())
	}
	return strings.Join(parts, "; ")
}

type policyGrantOutput ipc.PolicyGrantResult

func (o policyGrantOutput) Pretty() string {
	grant := o.Grant
	return fmt.Sprintf("grant %s alias=%s kind=%s expires_in=%s", grant.ID, grant.Alias, grant.Kind, expiresIn(grant.ExpiresAt))
}

type policyRevokeOutput ipc.PolicyRevokeResult

func (o policyRevokeOutput) Pretty() string {
	return fmt.Sprintf("removed=%d", o.Removed)
}

type policyListOutput ipc.PolicyListResult

func (o policyListOutput) Pretty() string {
	var b strings.Builder
	alias := o.Alias
	if alias == "" {
		alias = "default"
	}
	fmt.Fprintf(&b, "policy %s enabled=%t\n", alias, o.Policy.Enabled)
	writePatterns(&b, "command_whitelist", o.Policy.CommandWhitelist)
	writePatterns(&b, "command_blacklist", o.Policy.CommandBlacklist)
	writePatterns(&b, "local_path_whitelist", o.Policy.LocalPathWhitelist)
	writePatterns(&b, "remote_path_whitelist", o.Policy.RemotePathWhitelist)
	writePatterns(&b, "local_forward_whitelist", o.Policy.LocalForwardWhitelist)
	writePatterns(&b, "remote_forward_whitelist", o.Policy.RemoteForwardWhitelist)
	if len(o.Grants) == 0 {
		b.WriteString("grants: none")
		return b.String()
	}
	b.WriteString("grants:\n")
	for _, grant := range o.Grants {
		fmt.Fprintf(&b, "  %s alias=%s kind=%s expires_in=%s pattern=%s\n", grant.ID, grant.Alias, grant.Kind, expiresIn(grant.ExpiresAt), grant.Pattern)
	}
	return strings.TrimRight(b.String(), "\n")
}

type policyCheckOutput struct {
	Decision policypkg.Decision `json:"decision"`
}

func (o policyCheckOutput) Pretty() string {
	if o.Decision.Allowed {
		return "allowed"
	}
	return fmt.Sprintf("blocked reason=%s pattern=%s", o.Decision.Reason, o.Decision.Pattern)
}

func writePatterns(b *strings.Builder, name string, patterns []string) {
	if len(patterns) == 0 {
		fmt.Fprintf(b, "%s: []\n", name)
		return
	}
	fmt.Fprintf(b, "%s:\n", name)
	for _, pattern := range patterns {
		fmt.Fprintf(b, "  - %s\n", pattern)
	}
}

func expiresIn(unix int64) string {
	d := time.Until(time.Unix(unix, 0)).Truncate(time.Second)
	if d < 0 {
		return "expired"
	}
	return d.String()
}
