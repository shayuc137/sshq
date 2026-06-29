package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/shayuc137/sshq/internal/appconfig"
	"github.com/shayuc137/sshq/internal/config"
	"github.com/shayuc137/sshq/internal/credential"
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

func hostToConnConfig(host config.Host) sshclient.ConnConfig {
	cfg := sshclient.ConnConfig{
		Host:         host.HostName,
		Port:         host.Port,
		User:         host.User,
		IdentityFile: host.IdentityFile,
		ProxyJump:    host.ProxyJump,
	}
	return cfg
}

func hostToConnConfigWithStore(host config.Host, store *config.Store) sshclient.ConnConfig {
	return hostToConnConfigWithCredentials(host, store, nil)
}

func hostToConnConfigWithCredentials(host config.Host, store *config.Store, creds *credential.Store) sshclient.ConnConfig {
	cfg := hostToConnConfig(host)
	if creds != nil {
		if password, err := creds.Get(host.Alias); err == nil {
			cfg.Password = password
		}
	}
	if cfg.ProxyJump != "" && store != nil {
		cfg.ProxyConfig = resolveProxyChainWithCredentials(store, cfg.ProxyJump, creds)
	}
	return cfg
}

// resolveProxyChain recursively resolves a ProxyJump alias into a full
// ConnConfig chain. Handles multi-level jumps (A → B → C). A visited set
// guards against cyclic ProxyJump config (A → B → A), which would otherwise
// recurse until the stack overflows; on a cycle the chain is cut at the
// repeated host rather than panicking.
func resolveProxyChain(store *config.Store, proxyJump string) *sshclient.ConnConfig {
	return resolveProxyChainWithCredentials(store, proxyJump, nil)
}

func resolveProxyChainGuarded(store *config.Store, proxyJump string, visited map[string]bool) *sshclient.ConnConfig {
	return resolveProxyChainGuardedWithCredentials(store, proxyJump, visited, nil)
}

func resolveProxyChainWithCredentials(store *config.Store, proxyJump string, creds *credential.Store) *sshclient.ConnConfig {
	return resolveProxyChainGuardedWithCredentials(store, proxyJump, make(map[string]bool), creds)
}

func resolveProxyChainGuardedWithCredentials(store *config.Store, proxyJump string, visited map[string]bool, creds *credential.Store) *sshclient.ConnConfig {
	if proxyJump == "" || visited[proxyJump] {
		return nil
	}
	visited[proxyJump] = true
	proxy, err := store.Get(proxyJump)
	if err != nil {
		return nil
	}
	cfg := hostToConnConfig(proxy)
	if creds != nil {
		if password, err := creds.Get(proxy.Alias); err == nil {
			cfg.Password = password
		}
	}
	if proxy.ProxyJump != "" {
		cfg.ProxyConfig = resolveProxyChainGuardedWithCredentials(store, proxy.ProxyJump, visited, creds)
	}
	return &cfg
}

func connErrorToOutput(err error, alias string) *output.CmdError {
	var ce *sshclient.ConnError
	if !errors.As(err, &ce) {
		return output.Errorf(err.Error(), "check connectivity and credentials")
	}
	switch ce.Kind {
	case sshclient.ErrHostKeyMismatch:
		return output.Errorf(
			fmt.Sprintf("host key CHANGED for %s (%s:%s)", alias, ce.Host, ce.Port),
			fmt.Sprintf("if expected, run: sshq trust %s --replace", alias),
		)
	case sshclient.ErrHostKeyUnknown:
		return output.Errorf(
			fmt.Sprintf("host key unknown for %s (%s:%s)", alias, ce.Host, ce.Port),
			fmt.Sprintf("run: sshq trust %s", alias),
		)
	case sshclient.ErrAuth:
		return output.Errorf(ce.Error(), "check credentials and key file")
	case sshclient.ErrNetwork:
		return output.Errorf(ce.Error(), "check network connectivity")
	default:
		return output.Errorf(ce.Error(), "check connectivity and credentials")
	}
}
