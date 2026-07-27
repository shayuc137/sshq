package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"

	"github.com/shayuc137/sshq/internal/appconfig"
	"github.com/shayuc137/sshq/internal/audit"
	"github.com/shayuc137/sshq/internal/config"
	"github.com/shayuc137/sshq/internal/credential"
	"github.com/shayuc137/sshq/internal/hostkey"
	"github.com/shayuc137/sshq/internal/output"
	"github.com/shayuc137/sshq/internal/policy"
	"github.com/shayuc137/sshq/internal/remote"
	"github.com/shayuc137/sshq/internal/sshclient"
)

type writerKey struct{}
type configKey struct{}
type credentialStoreKey struct{}
type profileCacheKey struct{}
type appConfigKey struct{}
type appConfigErrorKey struct{}
type policyCheckerKey struct{}
type auditLoggerKey struct{}

func withWriter(ctx context.Context, w *output.Writer) context.Context {
	return context.WithValue(ctx, writerKey{}, w)
}

func writerFrom(ctx context.Context) *output.Writer {
	if w, ok := ctx.Value(writerKey{}).(*output.Writer); ok {
		return w
	}
	return output.New(os.Stdout, os.Stderr)
}

func withConfig(ctx context.Context, s *config.Store) context.Context {
	return context.WithValue(ctx, configKey{}, s)
}

func configFrom(ctx context.Context) *config.Store {
	if s, ok := ctx.Value(configKey{}).(*config.Store); ok {
		return s
	}
	return nil
}

func withCredentialStore(ctx context.Context, s *credential.Store) context.Context {
	return context.WithValue(ctx, credentialStoreKey{}, s)
}

func credentialStoreFrom(ctx context.Context) *credential.Store {
	if s, ok := ctx.Value(credentialStoreKey{}).(*credential.Store); ok {
		return s
	}
	return nil
}

func withProfileCache(ctx context.Context, c *remote.Cache) context.Context {
	return context.WithValue(ctx, profileCacheKey{}, c)
}

func profileCacheFrom(ctx context.Context) *remote.Cache {
	if c, ok := ctx.Value(profileCacheKey{}).(*remote.Cache); ok {
		return c
	}
	return nil
}

func withAppConfig(ctx context.Context, c *appconfig.Config) context.Context {
	return context.WithValue(ctx, appConfigKey{}, c)
}

func appConfigFrom(ctx context.Context) *appconfig.Config {
	if c, ok := ctx.Value(appConfigKey{}).(*appconfig.Config); ok {
		return c
	}
	return nil
}

func withAppConfigError(ctx context.Context, err error) context.Context {
	return context.WithValue(ctx, appConfigErrorKey{}, err)
}

func appConfigErrorFrom(ctx context.Context) error {
	if err, ok := ctx.Value(appConfigErrorKey{}).(error); ok {
		return err
	}
	return nil
}

func withPolicyChecker(ctx context.Context, c *policy.Checker) context.Context {
	return context.WithValue(ctx, policyCheckerKey{}, c)
}

func policyCheckerFrom(ctx context.Context) *policy.Checker {
	if c, ok := ctx.Value(policyCheckerKey{}).(*policy.Checker); ok {
		return c
	}
	return nil
}

func withAuditLogger(ctx context.Context, l *audit.Logger) context.Context {
	return context.WithValue(ctx, auditLoggerKey{}, l)
}

func auditLoggerFrom(ctx context.Context) *audit.Logger {
	if l, ok := ctx.Value(auditLoggerKey{}).(*audit.Logger); ok {
		return l
	}
	return nil
}

func recordAudit(ctx context.Context, entry audit.Entry) error {
	logger := auditLoggerFrom(ctx)
	if logger == nil {
		return nil
	}
	if err := logger.Record(entry); err != nil {
		return output.Errorf("audit log write failed: "+err.Error(), "check [audit] path and permissions").WithCode(output.CodeAuditWriteFailed)
	}
	return nil
}

func recordAuditError(ctx context.Context, entry audit.Entry, err error) error {
	if err != nil {
		entry.ErrorHint = audit.RedactSummary(err.Error())
	}
	return recordAudit(ctx, entry)
}

func hostToConnConfig(host config.Host) sshclient.ConnConfig {
	cfg := sshclient.ConnConfig{
		Alias:        host.Alias,
		Host:         host.HostName,
		Port:         host.Port,
		User:         host.User,
		IdentityFile: host.IdentityFile,
		ProxyJump:    host.ProxyJump,
	}
	return cfg
}

// hostToConnConfigWithCredentials builds a ConnConfig and attaches any stored
// password credential for the host (and its ProxyJump chain).
//
// Only credential.ErrNotFound is treated as "no password configured" and
// silently ignored; every other credential-store error (corrupt file, decrypt
// failure, insecure permissions, read error) is surfaced so it cannot be
// mistaken for a generic SSH authentication failure later.
func hostToConnConfigWithCredentials(host config.Host, store *config.Store, creds *credential.Store) (sshclient.ConnConfig, error) {
	cfg := hostToConnConfig(host)
	if creds != nil {
		password, err := lookupCredential(creds, host.Alias)
		if err != nil {
			return sshclient.ConnConfig{}, err
		}
		cfg.Password = password
	}
	if cfg.ProxyJump != "" && store != nil {
		proxyCfg, err := resolveProxyChainWithCredentials(store, cfg.ProxyJump, creds)
		if err != nil {
			return sshclient.ConnConfig{}, err
		}
		cfg.ProxyConfig = proxyCfg
	}
	return cfg, nil
}

// lookupCredential fetches a stored password for alias. A missing credential
// returns ("", nil); any real store error is returned as-is so callers can
// surface it.
func lookupCredential(creds *credential.Store, alias string) (string, error) {
	password, err := creds.Get(alias)
	if err != nil {
		if errors.Is(err, credential.ErrNotFound) {
			return "", nil
		}
		return "", err
	}
	return password, nil
}

// credentialErrorSummary maps a credential-store error to a short,
// password-free message for contexts that only have a string error channel
// (cluster result rows, profile probe warnings). credential sentinel errors
// never embed the secret value, so they are safe to surface verbatim.
func credentialErrorSummary(err error) string {
	switch {
	case errors.Is(err, credential.ErrCannotDecrypt):
		return "credential decrypt failed"
	case errors.Is(err, credential.ErrCorrupt):
		return "credential file corrupt"
	default:
		return "credential error: " + err.Error()
	}
}

func resolveProxyChainWithCredentials(store *config.Store, proxyJump string, creds *credential.Store) (*sshclient.ConnConfig, error) {
	return resolveProxyChainGuardedWithCredentials(store, proxyJump, make(map[string]bool), creds)
}

func resolveProxyChainGuardedWithCredentials(store *config.Store, proxyJump string, visited map[string]bool, creds *credential.Store) (*sshclient.ConnConfig, error) {
	if proxyJump == "" || visited[proxyJump] {
		return nil, nil
	}
	visited[proxyJump] = true
	proxy, err := store.Get(proxyJump)
	if err != nil {
		return nil, nil
	}
	cfg := hostToConnConfig(proxy)
	if creds != nil {
		password, err := lookupCredential(creds, proxy.Alias)
		if err != nil {
			return nil, err
		}
		cfg.Password = password
	}
	if proxy.ProxyJump != "" {
		proxyCfg, err := resolveProxyChainGuardedWithCredentials(store, proxy.ProxyJump, visited, creds)
		if err != nil {
			return nil, err
		}
		cfg.ProxyConfig = proxyCfg
	}
	return &cfg, nil
}

// runErrorToOutput classifies errors from the run phase, after a connection was
// established. The command may already have reached the remote host, so
// unclassified failures map to result_indeterminate — never internal_error —
// to stop callers from blindly retrying side-effectful commands.
func runErrorToOutput(err error) *output.CmdError {
	if errors.Is(err, context.DeadlineExceeded) {
		return output.Errorf(
			err.Error()+"; the remote command may still be running",
			"increase --timeout or run the command in background",
		).WithCode(output.CodeTimeout)
	}
	return output.Errorf(err.Error(), "verify remote state before retrying").
		WithCode(output.CodeResultIndeterminate)
}

func connErrorToOutput(err error, alias string) *output.CmdError {
	var ce *sshclient.ConnError
	if !errors.As(err, &ce) {
		if errors.Is(err, context.DeadlineExceeded) {
			return output.Errorf(
				err.Error()+"; the remote command may still be running",
				"increase --timeout or run the command in background",
			).WithCode(output.CodeTimeout)
		}
		var netErr net.Error
		if errors.As(err, &netErr) {
			return output.Errorf(err.Error(), "check network connectivity").WithCode(output.CodeNetworkError)
		}
		return output.Errorf(err.Error(), "check connectivity and credentials").WithCode(output.CodeInternalError)
	}
	errorAlias := ce.Alias
	if errorAlias == "" {
		errorAlias = alias
	}
	lookupKeys := ce.LookupKeys
	if len(lookupKeys) == 0 || ce.Alias == "" {
		lookupKeys = hostkey.LookupKeys(errorAlias, ce.Host, ce.Port)
	}
	details := hostKeyErrorDetails(
		errorAlias, ce.Host, ce.Port, ce.ProxyJump, lookupKeys,
		ce.RemoteFingerprint, ce.KnownFingerprint,
	)
	switch ce.Kind {
	case sshclient.ErrHostKeyMismatch:
		return output.Errorf(
			fmt.Sprintf("host key CHANGED for %s (%s:%s)", errorAlias, ce.Host, ce.Port),
			fmt.Sprintf("run: sshq trust %s --replace", errorAlias),
		).WithCode(output.CodeHostKeyMismatch).WithDetails(details)
	case sshclient.ErrHostKeyUnknown:
		return output.Errorf(
			fmt.Sprintf("host key unknown for %s (%s:%s)", errorAlias, ce.Host, ce.Port),
			fmt.Sprintf("run: sshq trust %s", errorAlias),
		).WithCode(output.CodeHostKeyUnknown).WithDetails(details)
	case sshclient.ErrAuth:
		return output.Errorf(ce.Error(), "check credentials and key file").WithCode(output.CodeAuthFailed)
	case sshclient.ErrNetwork:
		return output.Errorf(ce.Error(), "check network connectivity").WithCode(output.CodeNetworkError)
	default:
		return output.Errorf(ce.Error(), "check connectivity and credentials").WithCode(output.CodeInternalError)
	}
}

func hostKeyErrorDetails(alias, hostname, port, proxyJump string, lookupKeys []string, remoteFingerprint, knownFingerprint string) map[string]any {
	details := map[string]any{
		"alias":              alias,
		"hostname":           hostname,
		"port":               port,
		"proxy_jump":         proxyJump,
		"lookup_keys":        lookupKeys,
		"remote_fingerprint": remoteFingerprint,
	}
	if knownFingerprint != "" {
		details["known_fingerprint"] = knownFingerprint
	}
	return details
}
