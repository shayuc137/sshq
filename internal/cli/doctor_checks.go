package cli

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/shayuc137/sshq/internal/config"
	"github.com/shayuc137/sshq/internal/credential"
	"github.com/shayuc137/sshq/internal/hostkey"
	"github.com/shayuc137/sshq/internal/remote"
	"github.com/shayuc137/sshq/internal/sshclient"
)

type systemDoctorChecker struct {
	store   *config.Store
	creds   *credential.Store
	cache   *remote.Cache
	timeout time.Duration
}

func newSystemDoctorChecker(store *config.Store, creds *credential.Store, cache *remote.Cache, timeout time.Duration) doctorChecker {
	return &systemDoctorChecker{store: store, creds: creds, cache: cache, timeout: timeout}
}

func (c *systemDoctorChecker) Check(ctx context.Context, name doctorCheckName, state *doctorRunState) doctorCheckOutcome {
	switch name {
	case doctorConfigValid:
		return c.configValid(state)
	case doctorIdentityFile:
		return c.identityFileExists(state)
	case doctorProxyJump:
		return c.proxyJumpExists(state)
	case doctorTCPReachable:
		return c.tcpReachable(ctx, state)
	case doctorHostKeyKnown:
		return c.hostKeyKnown(state)
	case doctorAuthOK:
		return c.authOK(ctx, state)
	case doctorShellDetected:
		return c.shellDetected(ctx, state)
	default:
		return failedDoctorOutcome(doctorFailureConfigInvalid, "unknown doctor check", "", "")
	}
}

func (c *systemDoctorChecker) configValid(state *doctorRunState) doctorCheckOutcome {
	host, err := c.store.Get(state.alias)
	if err != nil {
		return failedDoctorOutcome(doctorFailureConfigMissing, err.Error(), "", "")
	}
	state.host = host
	if key := invalidDoctorConfigKey(host); key != "" {
		return failedDoctorOutcome(
			doctorFailureConfigInvalid,
			fmt.Sprintf("configuration value for %s has leading whitespace or unclosed quotes", key),
			key,
			"",
		)
	}
	return passedDoctorOutcome()
}

func invalidDoctorConfigKey(host config.Host) string {
	values := []struct {
		key   string
		value string
	}{
		{"hostname", host.HostName},
		{"port", host.Port},
		{"user", host.User},
		{"identityfile", host.IdentityFile},
		{"proxyjump", host.ProxyJump},
	}
	for _, field := range values {
		if strings.TrimLeft(field.value, " \t") != field.value ||
			strings.Count(field.value, `"`)%2 != 0 ||
			strings.Count(field.value, `'`)%2 != 0 {
			return field.key
		}
	}
	return ""
}

func (c *systemDoctorChecker) identityFileExists(state *doctorRunState) doctorCheckOutcome {
	if state.host.IdentityFile == "" {
		return doctorCheckOutcome{Value: doctorCheckNotApplicable}
	}
	path := credential.ExpandHome(state.host.IdentityFile)
	if _, err := os.Stat(path); err != nil {
		return failedDoctorOutcome(
			doctorFailureIdentityMissing,
			fmt.Sprintf("identity file %s is unavailable: %v", path, err),
			"",
			"",
		)
	}
	return passedDoctorOutcome()
}

func (c *systemDoctorChecker) proxyJumpExists(state *doctorRunState) doctorCheckOutcome {
	cfg, err := hostToConnConfigWithCredentials(state.host, c.store, c.creds)
	if err != nil {
		return failedDoctorOutcome(doctorFailureProxyMissing, credentialErrorSummary(err), "", state.host.ProxyJump)
	}
	cfg.Timeout = c.timeout
	state.connCfg = cfg
	if missing := unresolvedDoctorProxy(cfg); missing != "" {
		return failedDoctorOutcome(
			doctorFailureProxyMissing,
			fmt.Sprintf("ProxyJump alias %q is not configured", missing),
			"",
			missing,
		)
	}
	return passedDoctorOutcome()
}

func unresolvedDoctorProxy(cfg sshclient.ConnConfig) string {
	for cfg.ProxyJump != "" {
		if cfg.ProxyConfig == nil {
			return cfg.ProxyJump
		}
		cfg = *cfg.ProxyConfig
	}
	return ""
}

func (c *systemDoctorChecker) tcpReachable(ctx context.Context, state *doctorRunState) doctorCheckOutcome {
	conn, closer, err := sshclient.DialTCP(ctx, state.connCfg)
	if err != nil {
		hint := fmt.Sprintf("TCP connection to %s:%s failed", state.connCfg.Host, state.connCfg.Port)
		if state.connCfg.ProxyJump != "" {
			hint += fmt.Sprintf(" via %s; diagnose the jump host with sshq doctor %s", state.connCfg.ProxyJump, state.connCfg.ProxyJump)
		}
		return failedDoctorOutcome(doctorFailureTCP, hint+": "+err.Error(), "", "")
	}
	state.tcpConn = conn
	state.tcpCloser = closer
	return passedDoctorOutcome()
}

func (c *systemDoctorChecker) hostKeyKnown(state *doctorRunState) doctorCheckOutcome {
	addr := net.JoinHostPort(state.connCfg.Host, state.connCfg.Port)
	key, err := hostkey.FetchConn(state.tcpConn, addr, c.timeout)
	state.tcpCloser.Close()
	state.tcpCloser = nil
	state.tcpConn = nil
	if err != nil {
		return failedDoctorOutcome(doctorFailureHostKey, err.Error(), "", "")
	}
	result, err := hostkey.Check(addr, key)
	if err != nil {
		return failedDoctorOutcome(doctorFailureHostKey, err.Error(), "", "")
	}
	switch result.Status {
	case hostkey.Trusted:
		return passedDoctorOutcome()
	case hostkey.Unknown:
		return failedDoctorOutcome(doctorFailureHostKeyUnknown, "host key is not present in known_hosts", "", "")
	case hostkey.Mismatch:
		return failedDoctorOutcome(doctorFailureHostKeyMismatch, "host key differs from known_hosts", "", "")
	default:
		return failedDoctorOutcome(doctorFailureHostKey, "host key status is unknown", "", "")
	}
}

func (c *systemDoctorChecker) authOK(ctx context.Context, state *doctorRunState) doctorCheckOutcome {
	client, err := sshclient.Dial(ctx, state.connCfg)
	if err != nil {
		return failedDoctorOutcome(
			doctorFailureAuth,
			"SSH authentication failed after trying the agent, configured/default keys, and stored password when available: "+err.Error(),
			"",
			"",
		)
	}
	state.client = client
	return passedDoctorOutcome()
}

func (c *systemDoctorChecker) shellDetected(ctx context.Context, state *doctorRunState) doctorCheckOutcome {
	profile, err := remote.GetProfile(ctx, state.client, c.cache, state.connCfg.Host, state.connCfg.Port)
	if err != nil {
		return failedDoctorOutcome(
			doctorFailureShell,
			"remote shell detection failed; run sshq exec with an explicit --shell <bash|ash|powershell|cmd>: "+err.Error(),
			"",
			"",
		)
	}
	state.profile = profile
	return passedDoctorOutcome()
}

func passedDoctorOutcome() doctorCheckOutcome {
	return doctorCheckOutcome{Value: doctorCheckPassed}
}

func failedDoctorOutcome(kind doctorFailureKind, hint, key, valueHint string) doctorCheckOutcome {
	return doctorCheckOutcome{Value: doctorCheckFailed, Kind: kind, Hint: hint, Key: key, ValueHint: valueHint}
}
