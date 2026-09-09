package sshclient

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestAuthMethodsPublicKeyFallback(t *testing.T) {
	for _, mode := range []string{"empty-agent", "wrong-agent-key", "agent-only", "missing-agent", "broken-agent", "default-key", "password-fallback", "no-agent", "stalled-agent", "config-error", "dial-error", "handshake-error"} {
		t.Run(mode, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			_, private, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			signer, err := ssh.NewSignerFromKey(private)
			if err != nil {
				t.Fatal(err)
			}
			key, err := ssh.MarshalPrivateKey(private, "test")
			if err != nil {
				t.Fatal(err)
			}
			identity := filepath.Join(home, "identity")
			if mode != "agent-only" {
				path := identity
				if mode == "default-key" {
					if err := os.Mkdir(filepath.Join(home, ".ssh"), 0700); err != nil {
						t.Fatal(err)
					}
					path = filepath.Join(home, ".ssh", "id_ed25519")
					if err := os.WriteFile(identity, []byte("invalid key"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(path, pem.EncodeToMemory(key), 0600); err != nil {
					t.Fatal(err)
				}
			}
			// Keep Unix socket paths below platform limits despite long test names.
			dir, err := os.MkdirTemp("", "sshq-agent-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(dir) })
			sock := filepath.Join(dir, "agent")
			t.Setenv("SSH_AUTH_SOCK", sock)
			hasAgent := mode != "missing-agent" && mode != "no-agent"
			if mode == "no-agent" {
				t.Setenv("SSH_AUTH_SOCK", "")
			}
			agentClosed := make(chan struct{})
			if hasAgent {
				ln, err := net.Listen("unix", sock)
				if err != nil {
					t.Skipf("Unix sockets unavailable: %v", err)
				}
				t.Cleanup(func() { ln.Close() })
				keyring := agent.NewKeyring()
				if mode == "agent-only" || mode == "config-error" || mode == "dial-error" || mode == "handshake-error" {
					if err := keyring.Add(agent.AddedKey{PrivateKey: private}); err != nil {
						t.Fatal(err)
					}
				} else if mode == "wrong-agent-key" || mode == "password-fallback" {
					_, wrong, err := ed25519.GenerateKey(rand.Reader)
					if err != nil {
						t.Fatal(err)
					}
					if err := keyring.Add(agent.AddedKey{PrivateKey: wrong}); err != nil {
						t.Fatal(err)
					}
				}
				go func() {
					defer close(agentClosed)
					conn, err := ln.Accept()
					if err != nil {
						return
					}
					defer conn.Close()
					if mode == "stalled-agent" {
						_, _ = io.Copy(io.Discard, conn)
					} else if mode != "broken-agent" {
						_ = agent.ServeAgent(keyring, conn)
					}
				}()
			}
			var rejectedKeys int
			serverConfig := &ssh.ServerConfig{PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
				if mode != "password-fallback" && mode != "handshake-error" && bytes.Equal(key.Marshal(), signer.PublicKey().Marshal()) {
					return nil, nil
				}
				rejectedKeys++
				return nil, fmt.Errorf("unexpected key")
			}}
			if mode == "password-fallback" {
				serverConfig.PasswordCallback = func(_ ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
					if string(password) == "test-password" {
						return nil, nil
					}
					return nil, fmt.Errorf("unexpected password")
				}
			}
			serverConfig.AddHostKey(signer)
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			finished := make(chan error, 1)
			if mode != "config-error" && mode != "dial-error" {
				go func() {
					conn, err := ln.Accept()
					if err != nil {
						finished <- err
						return
					}
					defer conn.Close()
					_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
					s, _, _, err := ssh.NewServerConn(conn, serverConfig)
					if err == nil {
						s.Close()
					}
					finished <- err
				}()
			}
			knownHosts := filepath.Join(home, "known_hosts")
			if mode != "config-error" {
				line := knownhosts.Line([]string{knownhosts.Normalize(ln.Addr().String())}, signer.PublicKey())
				if err := os.WriteFile(knownHosts, []byte(line+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("SSHQ_KNOWN_HOSTS", knownHosts)
			host, port, err := net.SplitHostPort(ln.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			dialErr := errors.New("injected dial failure")
			if mode == "dial-error" {
				original := dialTCPContext
				t.Cleanup(func() { dialTCPContext = original })
				dialTCPContext = func(context.Context, string, time.Duration) (net.Conn, error) { return nil, dialErr }
			}
			timeout := 5 * time.Second
			if mode == "stalled-agent" {
				timeout = 100 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			client, err := Dial(ctx, ConnConfig{Host: host, Port: port, User: "test", IdentityFile: identity, Password: "test-password", Timeout: timeout})
			wantError := mode == "config-error" || mode == "dial-error" || mode == "handshake-error"
			if (err != nil) != wantError {
				t.Errorf("Dial error = %v, wantError = %v", err, wantError)
			}
			if mode == "dial-error" && !errors.Is(err, dialErr) {
				t.Errorf("Dial lost cause: %v", err)
			}
			if client != nil {
				defer client.Close()
			}
			if mode != "config-error" && mode != "dial-error" {
				if serverErr := <-finished; (serverErr != nil) != (mode == "handshake-error") {
					t.Errorf("server authentication: %v", serverErr)
				}
				if mode == "password-fallback" && rejectedKeys == 0 {
					t.Error("password fallback did not first reject a public key")
				}
			}
			// Check before closing the SSH client: Dial, not Client.Close, owns the agent.

			if hasAgent {
				select {
				case <-agentClosed:
				case <-time.After(time.Second):
					t.Error("agent socket was not closed after authentication")
				}
			}
		})
	}
}
