package sshclient

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

type ConnConfig struct {
	Host         string
	Port         string
	User         string
	IdentityFile string
	ProxyJump    string
	ProxyConfig  *ConnConfig
	Timeout      time.Duration
}

func Dial(ctx context.Context, cfg ConnConfig) (*ssh.Client, error) {
	if cfg.ProxyJump != "" {
		return dialViaProxy(ctx, cfg)
	}
	return dialDirect(ctx, cfg)
}

func dialDirect(ctx context.Context, cfg ConnConfig) (*ssh.Client, error) {
	sshCfg, err := buildSSHConfig(cfg)
	if err != nil {
		return nil, err
	}

	addr := net.JoinHostPort(cfg.Host, cfg.Port)

	dialer := net.Dialer{Timeout: cfg.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", addr, err)
	}

	return handshake(ctx, conn, addr, sshCfg, cfg)
}

func dialViaProxy(ctx context.Context, cfg ConnConfig) (*ssh.Client, error) {
	var proxyCfg ConnConfig
	if cfg.ProxyConfig != nil {
		proxyCfg = *cfg.ProxyConfig
	} else {
		var err error
		proxyCfg, err = resolveProxyConfig(cfg.ProxyJump)
		if err != nil {
			return nil, fmt.Errorf("resolve proxy %q: %w", cfg.ProxyJump, err)
		}
	}
	proxyCfg.Timeout = cfg.Timeout

	proxyClient, err := Dial(ctx, proxyCfg)
	if err != nil {
		return nil, fmt.Errorf("connect to proxy %s: %w", cfg.ProxyJump, err)
	}

	targetAddr := net.JoinHostPort(cfg.Host, cfg.Port)
	proxyConn, err := proxyClient.DialContext(ctx, "tcp", targetAddr)
	if err != nil {
		proxyClient.Close()
		return nil, fmt.Errorf("proxy %s → %s: %w", cfg.ProxyJump, targetAddr, err)
	}

	sshCfg, err := buildSSHConfig(cfg)
	if err != nil {
		proxyConn.Close()
		proxyClient.Close()
		return nil, err
	}

	client, err := handshake(ctx, proxyConn, targetAddr, sshCfg, cfg)
	if err != nil {
		proxyClient.Close()
		return nil, err
	}

	// Keep proxy alive: when the target client closes, the proxy conn is released
	// but the proxy client stays in the pool (if pooled) or gets GC'd (if direct).
	return client, nil
}

func resolveProxyConfig(proxyJump string) (ConnConfig, error) {
	// ProxyJump format: [user@]host[:port]
	proxy := ConnConfig{Port: "22"}

	s := proxyJump
	if idx := strings.LastIndex(s, "@"); idx >= 0 {
		proxy.User = s[:idx]
		s = s[idx+1:]
	}
	if idx := strings.LastIndex(s, ":"); idx >= 0 {
		proxy.Host = s[:idx]
		proxy.Port = s[idx+1:]
	} else {
		proxy.Host = s
	}

	if proxy.Host == "" {
		return ConnConfig{}, fmt.Errorf("empty proxy host in %q", proxyJump)
	}

	return proxy, nil
}

func buildSSHConfig(cfg ConnConfig) (*ssh.ClientConfig, error) {
	methods, err := authMethods(cfg)
	if err != nil {
		return nil, fmt.Errorf("auth setup: %w", err)
	}
	if len(methods) == 0 {
		return nil, fmt.Errorf("no authentication methods available for %s@%s", cfg.User, cfg.Host)
	}

	hostKeyCallback, err := hostKeyCallback()
	if err != nil {
		return nil, fmt.Errorf("host key verification: %w", err)
	}

	return &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            methods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         cfg.Timeout,
	}, nil
}

func handshake(ctx context.Context, conn net.Conn, addr string, sshCfg *ssh.ClientConfig, cfg ConnConfig) (*ssh.Client, error) {
	deadline, hasDeadline := ctx.Deadline()
	if hasDeadline {
		conn.SetDeadline(deadline)
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, sshCfg)
	if err != nil {
		conn.Close()
		return nil, categorizeError(err, cfg)
	}

	if hasDeadline {
		conn.SetDeadline(time.Time{})
	}

	return ssh.NewClient(sshConn, chans, reqs), nil
}

func authMethods(cfg ConnConfig) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	if m := agentAuth(); m != nil {
		methods = append(methods, m)
	}

	if cfg.IdentityFile != "" {
		m, err := keyAuth(cfg.IdentityFile)
		if err == nil {
			methods = append(methods, m)
		}
	}

	for _, name := range []string{"id_ed25519", "id_rsa"} {
		home, err := os.UserHomeDir()
		if err != nil {
			continue
		}
		path := filepath.Join(home, ".ssh", name)
		if path == cfg.IdentityFile {
			continue
		}
		m, err := keyAuth(path)
		if err == nil {
			methods = append(methods, m)
		}
	}

	return methods, nil
}

func agentAuth() ssh.AuthMethod {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil
	}
	return ssh.PublicKeysCallback(agent.NewClient(conn).Signers)
}

func keyAuth(path string) (ssh.AuthMethod, error) {
	if path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(home, path[2:])
	}

	key, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("parse key %s: %w", path, err)
	}

	return ssh.PublicKeys(signer), nil
}

func hostKeyCallback() (ssh.HostKeyCallback, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, ".ssh", "known_hosts")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("known_hosts not found at %s — sshq requires strict host key verification", path)
	}
	return knownhosts.New(path)
}

type ConnErrorKind int

const (
	ErrNetwork         ConnErrorKind = iota
	ErrAuth
	ErrHostKeyMismatch
	ErrHostKeyUnknown
	ErrGeneric
)

type ConnError struct {
	Kind  ConnErrorKind
	Host  string
	Port  string
	User  string
	Cause error
}

func (e *ConnError) Error() string {
	switch e.Kind {
	case ErrNetwork:
		return fmt.Sprintf("network error connecting to %s:%s", e.Host, e.Port)
	case ErrAuth:
		return fmt.Sprintf("authentication failed for %s@%s:%s", e.User, e.Host, e.Port)
	case ErrHostKeyMismatch:
		return fmt.Sprintf("host key CHANGED for %s:%s", e.Host, e.Port)
	case ErrHostKeyUnknown:
		return fmt.Sprintf("host key unknown for %s:%s", e.Host, e.Port)
	default:
		return fmt.Sprintf("SSH handshake with %s:%s failed", e.Host, e.Port)
	}
}

func (e *ConnError) Unwrap() error { return e.Cause }

func categorizeError(err error, cfg ConnConfig) error {
	ce := &ConnError{Host: cfg.Host, Port: cfg.Port, User: cfg.User, Cause: err}

	if _, ok := err.(*net.OpError); ok {
		ce.Kind = ErrNetwork
		return ce
	}

	errMsg := err.Error()
	if strings.Contains(errMsg, "unable to authenticate") || strings.Contains(errMsg, "no supported methods") {
		ce.Kind = ErrAuth
		return ce
	}
	if strings.Contains(errMsg, "knownhosts: key mismatch") {
		ce.Kind = ErrHostKeyMismatch
		return ce
	}
	if strings.Contains(errMsg, "knownhosts: key is unknown") ||
		strings.Contains(errMsg, "knownhosts") || strings.Contains(errMsg, "host key") {
		ce.Kind = ErrHostKeyUnknown
		return ce
	}

	ce.Kind = ErrGeneric
	return ce
}
