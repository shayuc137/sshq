package sshclient

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/shayuc137/sshq/internal/hostkey"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestResolveProxyConfig(t *testing.T) {
	tests := []struct {
		input    string
		wantHost string
		wantPort string
		wantUser string
	}{
		{"jumphost", "jumphost", "22", ""},
		{"user@jumphost", "jumphost", "22", "user"},
		{"user@jumphost:2222", "jumphost", "2222", "user"},
		{"jumphost:2222", "jumphost", "2222", ""},
		{"10.0.0.1", "10.0.0.1", "22", ""},
		{"admin@10.0.0.1:9527", "10.0.0.1", "9527", "admin"},
	}
	for _, tt := range tests {
		cfg, err := resolveProxyConfig(tt.input)
		if err != nil {
			t.Errorf("resolveProxyConfig(%q) error: %v", tt.input, err)
			continue
		}
		if cfg.Host != tt.wantHost {
			t.Errorf("resolveProxyConfig(%q).Host = %q, want %q", tt.input, cfg.Host, tt.wantHost)
		}
		if cfg.Port != tt.wantPort {
			t.Errorf("resolveProxyConfig(%q).Port = %q, want %q", tt.input, cfg.Port, tt.wantPort)
		}
		if cfg.User != tt.wantUser {
			t.Errorf("resolveProxyConfig(%q).User = %q, want %q", tt.input, cfg.User, tt.wantUser)
		}
	}
}

func TestResolveProxyConfigEmpty(t *testing.T) {
	_, err := resolveProxyConfig("")
	if err == nil {
		t.Error("expected error for empty proxy")
	}
}

func TestCategorizeHostKeyErrorDetails(t *testing.T) {
	remoteKey := newTestPublicKey(t)
	knownKey := newTestPublicKey(t)
	cfg := ConnConfig{Alias: "target", Host: "192.0.2.10", Port: "2222", ProxyJump: "jump"}

	for _, tt := range []struct {
		name    string
		want    []knownhosts.KnownKey
		kind    ConnErrorKind
		knownFP string
	}{
		{name: "unknown", kind: ErrHostKeyUnknown},
		{name: "mismatch", want: []knownhosts.KnownKey{{Key: knownKey}}, kind: ErrHostKeyMismatch, knownFP: hostkey.Fingerprint(knownKey)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := &hostKeyVerificationError{
				cause: &knownhosts.KeyError{Want: tt.want}, remoteKey: remoteKey,
			}
			got, ok := categorizeError(err, cfg).(*ConnError)
			if !ok {
				t.Fatalf("categorizeError returned %T", got)
			}
			if got.Kind != tt.kind || got.Alias != "target" || got.ProxyJump != "jump" {
				t.Fatalf("ConnError = %+v", got)
			}
			if got.RemoteFingerprint != hostkey.Fingerprint(remoteKey) || got.KnownFingerprint != tt.knownFP {
				t.Fatalf("fingerprints = remote %q known %q", got.RemoteFingerprint, got.KnownFingerprint)
			}
			if len(got.LookupKeys) != 3 || got.LookupKeys[2] != "[192.0.2.10]:2222" {
				t.Fatalf("lookup keys = %v", got.LookupKeys)
			}
		})
	}
}

func TestCategorizeConnectionError(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		kind ConnErrorKind
	}{
		{
			name: "dial error",
			err:  &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("network unreachable")},
			kind: ErrNetwork,
		},
		{
			name: "authentication error",
			err:  errors.New("ssh: unable to authenticate"),
			kind: ErrAuth,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := categorizeError(tt.err, ConnConfig{Alias: "target", Host: "192.0.2.10", Port: "22"}).(*ConnError)
			if !ok {
				t.Fatalf("categorizeError returned %T", got)
			}
			if got.Kind != tt.kind {
				t.Fatalf("ConnError.Kind = %v, want %v", got.Kind, tt.kind)
			}
			if !errors.Is(got, tt.err) {
				t.Fatal("categorized error does not preserve its cause")
			}
		})
	}
}

func newTestPublicKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	return signer.PublicKey()
}

// --- in-process SSH test server -------------------------------------------------

func newSSHServer(t *testing.T) (string, ssh.PublicKey) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	srvCfg := &ssh.ServerConfig{NoClientAuth: true}
	srvCfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback sockets unavailable: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				sconn, chans, reqs, err := ssh.NewServerConn(c, srvCfg)
				if err != nil {
					c.Close()
					return
				}
				go ssh.DiscardRequests(reqs)
				for nc := range chans {
					nc.Reject(ssh.Prohibited, "no channels in test server")
				}
				sconn.Close()
			}()
		}
	}()
	return ln.Addr().String(), signer.PublicKey()
}

func dialSSHRaw(addr string, hk ssh.PublicKey) (*ssh.Client, error) {
	return ssh.Dial("tcp", addr, &ssh.ClientConfig{
		User:            "test",
		HostKeyCallback: ssh.FixedHostKey(hk),
		Timeout:         5 * time.Second,
	})
}

func mustDial(t *testing.T, addr string, hk ssh.PublicKey) *ssh.Client {
	t.Helper()
	c, err := dialSSHRaw(addr, hk)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// recordCloser is a test io.Closer that counts how many times it is closed.
type recordCloser struct{ closed int }

func (r *recordCloser) Close() error {
	r.closed++
	return nil
}

func TestDialTCPDirect(t *testing.T) {
	originalDial := dialTCPContext
	t.Cleanup(func() { dialTCPContext = originalDial })
	client, peer := net.Pipe()
	dialTCPContext = func(ctx context.Context, addr string, timeout time.Duration) (net.Conn, error) {
		if addr != "target.example:2222" {
			t.Fatalf("addr = %q", addr)
		}
		if timeout != 3*time.Second {
			t.Fatalf("timeout = %s", timeout)
		}
		return client, nil
	}
	peerClosed := make(chan struct{})
	go func() {
		defer peer.Close()
		io.Copy(io.Discard, peer)
		close(peerClosed)
	}()

	conn, closer, err := DialTCP(context.Background(), ConnConfig{Host: "target.example", Port: "2222", Timeout: 3 * time.Second})
	if err != nil {
		t.Fatalf("DialTCP: %v", err)
	}
	if conn == nil || closer == nil {
		t.Fatal("DialTCP must return a connection and a non-nil closer")
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("second Close must be idempotent: %v", err)
	}
	select {
	case <-peerClosed:
	case <-time.After(time.Second):
		t.Fatal("direct peer did not observe connection close")
	}
}

func TestDialTCPContextCancel(t *testing.T) {
	originalDial := dialTCPContext
	t.Cleanup(func() { dialTCPContext = originalDial })
	dialTCPContext = func(ctx context.Context, _ string, _ time.Duration) (net.Conn, error) {
		return nil, ctx.Err()
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	conn, closer, err := DialTCP(ctx, ConnConfig{Host: "target.example", Port: "22", Timeout: time.Hour})
	if conn != nil || closer != nil {
		t.Fatal("cancelled dial returned connection resources")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("DialTCP error = %v, want context.Canceled", err)
	}
}

type fakeProxyDialer struct {
	conn   net.Conn
	closed int
}

func (f *fakeProxyDialer) DialContext(_ context.Context, network, addr string) (net.Conn, error) {
	if network != "tcp" || addr != "target.internal:22" {
		return nil, errors.New("unexpected proxy dial target")
	}
	return f.conn, nil
}

func (f *fakeProxyDialer) Close() error {
	f.closed++
	return nil
}

func TestDialTCPViaProxyCloserCascades(t *testing.T) {
	originalDialProxy := dialProxyTest
	t.Cleanup(func() { dialProxyTest = originalDialProxy })
	targetConn, targetPeer := net.Pipe()
	proxy := &fakeProxyDialer{conn: targetConn}
	dialProxyTest = func(_ context.Context, cfg ConnConfig) (proxyDialer, error) {
		if cfg.Host != "jump.example" || cfg.Timeout != 4*time.Second {
			t.Fatalf("proxy config = %+v", cfg)
		}
		return proxy, nil
	}
	targetClosed := make(chan struct{})
	go func() {
		defer targetPeer.Close()
		io.Copy(io.Discard, targetPeer)
		close(targetClosed)
	}()

	proxyCfg := ConnConfig{Host: "jump.example", Port: "22"}
	conn, closer, err := DialTCP(context.Background(), ConnConfig{
		Host: "target.internal", Port: "22", ProxyJump: "jump", ProxyConfig: &proxyCfg, Timeout: 4 * time.Second,
	})
	if err != nil {
		t.Fatalf("DialTCP via proxy: %v", err)
	}
	if conn != targetConn {
		t.Fatal("DialTCP did not return the proxied target connection")
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("second Close must be idempotent: %v", err)
	}

	if proxy.closed != 1 {
		t.Fatalf("proxy close count = %d, want 1", proxy.closed)
	}
	select {
	case <-targetClosed:
	case <-time.After(time.Second):
		t.Fatal("target connection was not closed by the cascading closer")
	}
}

// TestClientCloseCascadesProxy is the R1 regression guard: Client.Close must
// release both the SSH connection and the proxy hop it was dialed through, so a
// proxied dial does not leak its jump connection.
func TestClientCloseCascadesProxy(t *testing.T) {
	addr, hk := newSSHServer(t)
	target := mustDial(t, addr, hk)
	proxyClient := mustDial(t, addr, hk)

	// Build the wrapper exactly as dialViaProxy would: target wrapped, with the
	// proxy hop stored as its proxy closer.
	c := newClient(target, ConnConfig{}, proxyClient)

	// While the wrapper is open the proxy hop must stay open.
	if _, _, err := proxyClient.SendRequest("keepalive@openssh.com", true, nil); err != nil {
		t.Fatalf("proxy closed prematurely while target was open: %v", err)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	done := make(chan struct{})
	go func() {
		proxyClient.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("proxy client was not closed after the wrapper closed (cascade leak)")
	}

	if _, _, err := target.SendRequest("keepalive@openssh.com", true, nil); err == nil {
		t.Error("target connection should be closed after Close")
	}
}

// TestClientCloseCascadesNestedChain verifies that a multi-hop ProxyJump chain —
// modelled as nested *Client proxy closers — is released layer by layer by a
// single Close on the outermost client.
func TestClientCloseCascadesNestedChain(t *testing.T) {
	addr, hk := newSSHServer(t)
	inner := mustDial(t, addr, hk)
	mid := mustDial(t, addr, hk)
	outerRaw := mustDial(t, addr, hk)

	innerWrapped := newClient(inner, ConnConfig{}, nil)
	midWrapped := newClient(mid, ConnConfig{}, innerWrapped)
	outer := newClient(outerRaw, ConnConfig{}, midWrapped)

	if err := outer.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	for name, c := range map[string]*ssh.Client{"inner": inner, "mid": mid, "outer": outerRaw} {
		if _, _, err := c.SendRequest("keepalive@openssh.com", true, nil); err == nil {
			t.Errorf("%s connection should be closed after cascading Close", name)
		}
	}
}

// TestClientCloseInvokesProxyCloserOnce pins the contract that the proxy closer
// is invoked exactly once per Close.
func TestClientCloseInvokesProxyCloserOnce(t *testing.T) {
	addr, hk := newSSHServer(t)
	target := mustDial(t, addr, hk)

	rec := &recordCloser{}
	c := newClient(target, ConnConfig{}, rec)
	if err := c.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if rec.closed != 1 {
		t.Errorf("proxy closer invoked %d times, want 1", rec.closed)
	}
}

// TestClientCloseDirectProxyNilSafe verifies a direct (proxy-less) client closes
// cleanly without dereferencing a nil proxy.
func TestClientCloseDirectProxyNilSafe(t *testing.T) {
	addr, hk := newSSHServer(t)
	target := mustDial(t, addr, hk)

	c := newClient(target, ConnConfig{}, nil)
	if err := c.Close(); err != nil {
		t.Fatalf("Close on a direct client must not error: %v", err)
	}
}
