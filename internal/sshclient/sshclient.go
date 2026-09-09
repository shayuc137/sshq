package sshclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/shayuc137/sshq/internal/hostkey"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

type ConnConfig struct {
	Alias        string
	Host         string
	Port         string
	User         string
	IdentityFile string
	Password     string
	ProxyJump    string
	ProxyConfig  *ConnConfig
	Timeout      time.Duration
}

// Client wraps *ssh.Client with connection metadata and deterministic lifecycle
// management. Embedding *ssh.Client promotes every existing method (Dial, Listen,
// Wait, SendRequest, NewSession, …) so callers use it exactly like a raw client.
type Client struct {
	*ssh.Client
	Alias     string
	Config    ConnConfig
	CreatedAt time.Time
	// proxy is the closer for the jump connection this client was dialed through
	// (nil for direct connections). For nested ProxyJump chains it is the inner
	// *Client, so Close cascades down the entire chain.
	proxy io.Closer
}

func newClient(raw *ssh.Client, cfg ConnConfig, proxy io.Closer) *Client {
	return &Client{
		Client:    raw,
		Alias:     cfg.Alias,
		Config:    cfg,
		CreatedAt: time.Now(),
		proxy:     proxy,
	}
}

// Close tears down the SSH connection and, if this client was dialed through a
// proxy hop, cascade-closes that hop. An ssh.Client owns a live TCP connection
// the GC will not reclaim on its own, so the proxy chain must be closed
// explicitly; otherwise every proxied dial leaks its jump connection(s).
func (c *Client) Close() error {
	err := c.Client.Close()
	if c.proxy != nil {
		c.proxy.Close()
	}
	return err
}

func Dial(ctx context.Context, cfg ConnConfig) (*Client, error) {
	sshCfg, cleanup, err := buildSSHConfig(cfg)
	defer cleanup()
	if err != nil {
		return nil, err
	}

	addr := net.JoinHostPort(cfg.Host, cfg.Port)
	conn, closer, err := DialTCP(ctx, cfg)
	if err != nil {
		return nil, err
	}

	raw, err := handshake(ctx, conn, addr, sshCfg, cfg)
	if err != nil {
		closer.Close()
		return nil, err
	}
	return newClient(raw, cfg, closer), nil
}

// DialTCP returns a TCP connection to cfg's target through the same direct or
// ProxyJump path used by Dial. Closing closer tears down the connection and the
// complete tunnel chain; the closer is always non-nil and idempotent.
func DialTCP(ctx context.Context, cfg ConnConfig) (net.Conn, io.Closer, error) {
	if cfg.ProxyJump == "" {
		addr := net.JoinHostPort(cfg.Host, cfg.Port)
		conn, err := dialTCPContext(ctx, addr, cfg.Timeout)
		if err != nil {
			return nil, nil, categorizeError(err, cfg)
		}
		return conn, newDialCloser(conn, nil), nil
	}

	var proxyCfg ConnConfig
	if cfg.ProxyConfig != nil {
		proxyCfg = *cfg.ProxyConfig
	} else {
		var err error
		proxyCfg, err = resolveProxyConfig(cfg.ProxyJump)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve proxy %q: %w", cfg.ProxyJump, err)
		}
	}
	proxyCfg.Timeout = cfg.Timeout

	var proxyClient proxyDialer
	var err error
	if dialProxyTest != nil {
		proxyClient, err = dialProxyTest(ctx, proxyCfg)
	} else {
		proxyClient, err = Dial(ctx, proxyCfg)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("connect to proxy %s: %w", cfg.ProxyJump, err)
	}

	targetAddr := net.JoinHostPort(cfg.Host, cfg.Port)
	proxyConn, err := proxyClient.DialContext(ctx, "tcp", targetAddr)
	if err != nil {
		proxyClient.Close()
		return nil, nil, fmt.Errorf("proxy %s → %s: %w", cfg.ProxyJump, targetAddr, err)
	}

	return proxyConn, newDialCloser(proxyConn, proxyClient), nil
}

type proxyDialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
	io.Closer
}

var dialTCPContext = func(ctx context.Context, addr string, timeout time.Duration) (net.Conn, error) {
	return (&net.Dialer{Timeout: timeout}).DialContext(ctx, "tcp", addr)
}

var dialProxyTest func(context.Context, ConnConfig) (proxyDialer, error)

type dialCloser struct {
	once sync.Once
	err  error
	conn io.Closer
	tail io.Closer
}

func newDialCloser(conn io.Closer, tail io.Closer) *dialCloser {
	return &dialCloser{conn: conn, tail: tail}
}

func (c *dialCloser) Close() error {
	c.once.Do(func() {
		var tailErr error
		if c.tail != nil {
			tailErr = c.tail.Close()
		}
		c.err = errors.Join(c.conn.Close(), tailErr)
	})
	return c.err
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
	proxy.Alias = proxy.Host

	return proxy, nil
}

func buildSSHConfig(cfg ConnConfig) (*ssh.ClientConfig, func(), error) {
	methods, cleanup := authMethods(cfg)
	if len(methods) == 0 {
		return nil, cleanup, fmt.Errorf("no authentication methods available for %s@%s", cfg.User, cfg.Host)
	}

	hostKeyCallback, err := hostKeyCallback()
	if err != nil {
		return nil, cleanup, fmt.Errorf("host key verification: %w", err)
	}

	return &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            methods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         cfg.Timeout,
	}, cleanup, nil
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

func authMethods(cfg ConnConfig) ([]ssh.AuthMethod, func()) {
	var methods []ssh.AuthMethod
	signers, cleanup := agentSigners(cfg.Timeout)

	if cfg.IdentityFile != "" {
		m, err := keySigner(cfg.IdentityFile)
		if err == nil {
			signers = append(signers, m)
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
		m, err := keySigner(path)
		if err == nil {
			signers = append(signers, m)
		}
	}
	// x/crypto/ssh tries each method name only once. Separate publickey
	// methods would let an empty or rejected agent hide valid file keys.
	if len(signers) > 0 {
		methods = append(methods, ssh.PublicKeys(signers...))
	}

	if cfg.Password != "" {
		methods = append(methods, ssh.Password(cfg.Password))
	}

	return methods, cleanup
}

func agentSigners(timeout time.Duration) ([]ssh.Signer, func()) {
	noop := func() {}
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, noop
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	conn, err := net.DialTimeout("unix", sock, timeout)
	if err != nil {
		return nil, noop
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	signers, err := agent.NewClient(conn).Signers()
	if err != nil || len(signers) == 0 {
		conn.Close()
		return nil, noop
	}
	// Agent signers use this socket during authentication, but never need it
	// after the handshake. Dial owns cleanup on success and every error path.
	return signers, func() { conn.Close() }
}

func keySigner(path string) (ssh.Signer, error) {
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

	return signer, nil
}

func hostKeyCallback() (ssh.HostKeyCallback, error) {
	path, err := hostkey.Path()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("known_hosts not found at %s — sshq requires strict host key verification", path)
	}
	callback, err := knownhosts.New(path)
	if err != nil {
		return nil, err
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if err := callback(hostname, remote, key); err != nil {
			return &hostKeyVerificationError{cause: err, remoteKey: key}
		}
		return nil
	}, nil
}

type hostKeyVerificationError struct {
	cause     error
	remoteKey ssh.PublicKey
}

func (e *hostKeyVerificationError) Error() string { return e.cause.Error() }
func (e *hostKeyVerificationError) Unwrap() error { return e.cause }

type ConnErrorKind int

const (
	ErrNetwork ConnErrorKind = iota
	ErrAuth
	ErrHostKeyMismatch
	ErrHostKeyUnknown
	ErrGeneric
)

type ConnError struct {
	Kind              ConnErrorKind
	Alias             string
	Host              string
	Port              string
	User              string
	ProxyJump         string
	LookupKeys        []string
	RemoteFingerprint string
	KnownFingerprint  string
	Cause             error
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
	ce := &ConnError{
		Alias: cfg.Alias, Host: cfg.Host, Port: cfg.Port, User: cfg.User, ProxyJump: cfg.ProxyJump,
		LookupKeys: hostkey.LookupKeys(cfg.Alias, cfg.Host, cfg.Port), Cause: err,
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		ce.Kind = ErrNetwork
		return ce
	}

	var verificationErr *hostKeyVerificationError
	if errors.As(err, &verificationErr) {
		ce.RemoteFingerprint = hostkey.Fingerprint(verificationErr.remoteKey)
		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) {
			if len(keyErr.Want) == 0 {
				ce.Kind = ErrHostKeyUnknown
			} else {
				ce.Kind = ErrHostKeyMismatch
				ce.KnownFingerprint = hostkey.Fingerprint(keyErr.Want[0].Key)
			}
			return ce
		}
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
