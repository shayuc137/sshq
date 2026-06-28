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

// TestBindProxyLifecycleClosesProxyWhenTargetCloses is the R3 regression guard:
// the proxy hop's connection must be released when the target client closes, so
// a proxied dial does not leak the jump connection.
func TestBindProxyLifecycleClosesProxyWhenTargetCloses(t *testing.T) {
	addr, hk := newSSHServer(t)
	target, err := dialSSHRaw(addr, hk)
	if err != nil {
		t.Fatal(err)
	}
	proxyClient, err := dialSSHRaw(addr, hk)
	if err != nil {
		t.Fatal(err)
	}

	bindProxyLifecycle(target, proxyClient)

	// While the target is open the proxy hop must stay open.
	if _, _, err := proxyClient.SendRequest("keepalive@openssh.com", true, nil); err != nil {
		t.Fatalf("proxy closed prematurely while target was open: %v", err)
	}

	// Closing the target must cascade-close the proxy hop.
	target.Close()

	done := make(chan struct{})
	go func() {
		proxyClient.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("proxy client was not closed after the target client closed (R3 leak)")
	}
}
