package sshclient

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
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
		t.Fatal(err)
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
